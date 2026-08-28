package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/wow-look-at-my/dats/schema"
)

// Runner executes tests from .dats files
type Runner struct {
	Verbose   bool
	KeepTemp  bool   // Keep temp directory for debugging
	CoverDir  string // Directory for GOCOVERDIR coverage data
	Formatter *Formatter
	// Update rewrites snapshot golden files from actual output instead of
	// failing mismatches, and prunes stale goldens (the --update flag).
	Update bool
	// Sandbox selects the sandbox every command runs under. Nil runs
	// commands on the host: this is the raw runner, so the safe default
	// lives one layer up -- both the CLI and dats.Run pass a config unless
	// the caller explicitly opted out.
	// Backend detection inside it is memoized, so sharing one config across
	// files and workers probes the host at most once.
	Sandbox *SandboxConfig

	// Env are extra KEY=VALUE entries applied to every command this runner
	// executes -- test instances and file-level hooks alike -- on top of the
	// inherited environment. A test's own inputs.env entries are applied
	// after these, so a file can still override what the caller set. An
	// entry with an empty value clears the inherited variable, which is how
	// a caller strips plumbing its children must not inherit.
	Env []string

	// plan is the resolved sandbox for the file currently being run, set by
	// RunFile/runFileParallel before any of that file's commands execute (nil
	// = run on the host). Per-file rather than per-run because the sandbox is
	// a file-level declaration; safe to hold on the Runner because a Runner
	// only ever runs one file at a time -- jobs mode gives each file its own.
	plan *sandboxPlan

	// sourceDir is the directory holding the .dats file currently being run,
	// set by RunFile/runFileParallel alongside plan. It resolves a relative
	// inputs.copy/shared.copy source, so a copy fixture is portable
	// regardless of dats' own working directory.
	sourceDir string

	// SSH runs every command of the run on another machine. Nil (the
	// default) runs them here. The remote shell is then the whole boundary:
	// dats installs no sandbox there.
	SSH *SSHConfig

	// remoteBase mirrors the current file's temp directory on the target,
	// set by runFile alongside plan. Empty when the run is local.
	remoteBase string

	// lowPriority runs every spawned workload command -- test instances and
	// file-level setup/teardown hooks alike -- at low OS priority (unix nice
	// 19, best-effort; no-op on windows). Only the multi-file orchestration
	// (RunFiles) sets it: a many-file run can saturate the machine, while a
	// direct RunFile call is a single file the caller asked for.
	lowPriority bool
}

// NewRunner creates a new test runner
func NewRunner(output io.Writer, verbose bool, keepTemp bool, coverDir string) *Runner {
	return &Runner{
		Verbose:  verbose,
		KeepTemp: keepTemp,
		CoverDir: coverDir,
		Formatter: &Formatter{
			Writer:  output,
			Verbose: verbose,
		},
	}
}

// RunFile runs all tests in a .dats file. Matrix tests are first expanded
// into one instance per value combination -- every instance always runs;
// there is no test filtering or selection. Shared fixture files are written
// and setup commands run first; then every test instance; then teardown
// commands, which always run -- after test failures and even when setup
// failed. A setup failure fails every test instance in the file (loudly;
// tests are never reported as skipped), and a teardown failure marks the
// file failed even when all tests passed.
//
// Canceling ctx kills in-flight setup and test commands (whole process
// groups) promptly; teardown still runs -- see the context.WithoutCancel
// call below.
func (r *Runner) RunFile(ctx context.Context, path string) (*FileResult, error) {
	testFile, err := schema.ParseFile(path)
	if err != nil {
		return nil, err
	}
	// A single-file run still goes through the pool -- it is the only
	// execution path -- so it gets its own, sized to this machine. RunFiles
	// shares ONE pool across every file instead, which is what bounds a
	// multi-file run globally.
	return r.runFile(ctx, path, testFile, newSlots(runtime.NumCPU()))
}

// runFile runs one already-parsed file against a caller-provided pool. The
// pool is what makes the concurrency global: RunFiles hands every file the
// same one, so N bounds the whole run rather than each file separately.
func (r *Runner) runFile(ctx context.Context, path string, testFile *schema.TestFile, pool slots) (*FileResult, error) {
	// Create temp directory for fixtures
	tempDir, err := os.MkdirTemp("", "dats-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp directory: %w", err)
	}
	// Resolve it to the path the kernel actually uses, ONCE and here, so the
	// sandbox's bind mounts and the {inputs.X}/{outputs.X} paths inside the
	// command can never disagree. On macOS MkdirTemp hands back /tmp/dats-*
	// while /tmp is a symlink to /private/tmp; docker shares the real path,
	// so binding the unresolved one mounts an empty directory -- fixtures
	// never arrive and outputs never land back on the host. A no-op wherever
	// the temp path is already real.
	if resolved, rerr := filepath.EvalSymlinks(tempDir); rerr == nil {
		tempDir = resolved
	}
	if !r.KeepTemp {
		defer Cleanup(tempDir)
	} else {
		fmt.Fprintf(r.Formatter.Writer, "# Temp directory: %s\n", tempDir)
	}

	// Ensure coverage directory exists if specified
	if r.CoverDir != "" {
		if err := os.MkdirAll(r.CoverDir, 0o755); err != nil {
			return nil, fmt.Errorf("creating coverage directory: %w", err)
		}
	}

	// Claim the file's directory on the ssh target before anything runs, for
	// the same reason the sandbox resolves here: a file that cannot reach
	// where its commands must run fails outright.
	if r.SSH != nil {
		if err := r.SSH.Connect(ctx); err != nil {
			return nil, err
		}
		if r.remoteBase, err = r.SSH.AllocBase(ctx); err != nil {
			return nil, err
		}
		if !r.KeepTemp {
			defer r.SSH.RemoveBase(r.remoteBase)
		} else {
			fmt.Fprintf(r.Formatter.Writer, "# Remote temp directory: %s:%s\n", r.SSH.Target, r.remoteBase)
		}
	}

	// Resolve the file's sandbox before anything runs: a file that must be
	// sandboxed and cannot be fails outright, rather than quietly running its
	// commands on the host.
	if r.plan, err = r.newSandboxPlan(testFile.Sandbox, tempDir); err != nil {
		return nil, err
	}
	if r.sourceDir, err = sourceDirOf(path); err != nil {
		return nil, err
	}

	// Expand every test into its matrix instances up front, so instance
	// numbering, per-instance temp directories, the header's test count, the
	// summary counts, and setup-failure reporting all operate on the expanded
	// list. A file with a 2x2 matrix test and one plain test runs 5 tests.
	instances := make([]schema.TestInstance, 0, len(testFile.Tests))
	for i := range testFile.Tests {
		instances = append(instances, schema.ExpandMatrix(&testFile.Tests[i])...)
	}

	r.Formatter.PrintSandbox(r.plan.describe(), r.Sandbox.TakeProcNotice())
	r.Formatter.PrintHeader(path, len(instances))

	result := &FileResult{
		Path:    path,
		Results: make([]TestResult, 0, len(instances)),
	}

	// The file's shared directory exists for the whole run (and is preserved
	// by --keep-temp) so every {shared.X} placeholder resolves to a writable
	// path, whether or not the file declares shared fixtures.
	sharedDir := filepath.Join(tempDir, sharedDirName)
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		return nil, fmt.Errorf("creating shared dir: %w", err)
	}

	// Write shared fixture files, then run setup commands in declared order,
	// stopping at the first failure. A failure here fails every test in the
	// file; teardown still runs.
	if testFile.Shared != nil {
		if err := SetupSharedFixtures(sharedDir, testFile.Shared.Files, testFile.Shared.Copy, r.sourceDir); err != nil {
			result.SetupFailure = &CommandFailure{Detail: fmt.Sprintf("shared fixtures: %v", err)}
			r.Formatter.PrintHookFailure("setup", result.SetupFailure)
		}
	}
	// hookSharedDir is the shared directory a hook command SEES. Shared
	// fixtures are built locally and copied over, so setup finds them there.
	// The push happens even with no shared block: {shared.X} must resolve to
	// a real directory whether or not the file declares one.
	hookSharedDir := sharedDir
	if r.SSH != nil && result.SetupFailure == nil {
		hookSharedDir = remoteJoin(r.remoteBase, sharedDirName)
		if err := r.SSH.Push(ctx, sharedDir, hookSharedDir); err != nil {
			result.SetupFailure = &CommandFailure{Detail: err.Error()}
			r.Formatter.PrintHookFailure("setup", result.SetupFailure)
		}
	}
	if result.SetupFailure == nil {
		for _, hc := range testFile.Setup {
			// Hook commands hold a pool slot exactly like instance commands,
			// so N bounds every spawned process, not just the test ones.
			pool.acquire()
			fail := r.runHookCommand(ctx, "setup", hc, hookSharedDir)
			pool.release()
			if fail != nil {
				result.SetupFailure = fail
				r.Formatter.PrintHookFailure("setup", fail)
				break // remaining setup commands are skipped; every test fails below
			}
		}
	}

	if result.SetupFailure != nil {
		// Every test instance is reported as a normal failure -- loudly,
		// never as "skipped" -- so the plan, the counts, and the exit code
		// all stay consistent.
		fmt.Fprintln(r.Formatter.Writer)
		for i := range instances {
			testResult := TestResult{
				Name:     instanceName(&instances[i]),
				Index:    i,
				Failures: []string{"file setup failed"},
			}
			result.Results = append(result.Results, testResult)
			result.Failed++
			r.Formatter.PrintResult(&testResult)
		}
	} else {
		if r.Verbose && len(testFile.Setup) > 0 {
			fmt.Fprintln(r.Formatter.Writer)
		}
		// Launch every instance; the pool bounds how many run at once. Each
		// writes only its own slice element, its own test-<index> directory,
		// its own (per-instance unique) golden files, and (by convention,
		// read-only) the file's shared directory. Snapshot assertions apply
		// after the instance name is set -- the golden file name derives from
		// it. Results land at their canonical index, so instance numbering and
		// print order follow expansion order regardless of completion order.
		instanceResults := make([]TestResult, len(instances))
		var wg sync.WaitGroup
		for i := range instances {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				pool.acquire()
				defer pool.release()
				testResult := r.RunTest(ctx, &instances[i].Test, tempDir, i)
				testResult.Name = instanceName(&instances[i])
				r.applySnapshot(&testResult, &instances[i], path, tempDir, i)
				instanceResults[i] = testResult
			}(i)
		}
		wg.Wait()

		for i := range instanceResults {
			result.Results = append(result.Results, instanceResults[i])
			r.Formatter.PrintResult(&instanceResults[i])

			if instanceResults[i].Passed {
				result.Passed++
			} else {
				result.Failed++
			}
		}
	}

	// Under --update, stale golden files (instances or streams that no
	// longer exist) are pruned after the instance loop -- but never after a
	// setup failure, when nothing ran and nothing is authoritative.
	if r.Update && result.SetupFailure == nil {
		r.pruneStaleGoldens(result, instances, path)
		r.Formatter.PrintPrunedGoldens(result)
	}

	// Teardown always runs: in declared order, after test failures, and even
	// when setup failed. One failing command does not stop the rest; any
	// failure marks the file failed. Teardown commands run under
	// context.WithoutCancel: the file-format contract says teardown ALWAYS
	// runs, including when a watch run is interrupted -- a canceled ctx must
	// kill in-flight setup/test commands but never the cleanup.
	teardownCtx := context.WithoutCancel(ctx)
	if r.Verbose && len(testFile.Teardown) > 0 {
		fmt.Fprintln(r.Formatter.Writer)
	}
	for _, hc := range testFile.Teardown {
		pool.acquire()
		fail := r.runHookCommand(teardownCtx, "teardown", hc, hookSharedDir)
		pool.release()
		if fail != nil {
			result.TeardownFailures = append(result.TeardownFailures, *fail)
			r.Formatter.PrintHookFailure("teardown", fail)
		}
	}

	r.Formatter.PrintSummary(result)

	return result, nil
}

// runHookCommand executes one file-level setup or teardown command through
// the same bash path as test commands (same working directory convention,
// inherited environment -- including GOCOVERDIR under --coverdir, exactly
// like test commands -- plus hc's own env and stdin_file), expanding hc.Cmd
// and hc.Env values through {shared.X} only. The command is bounded by
// hc.EffectiveTimeout(): unlike a test's timeout, a hook command always has
// one. It returns nil on success (exit 0) or the failure otherwise. Callers
// pass the live ctx for setup and a context.WithoutCancel ctx for teardown,
// which must run even after cancellation.
func (r *Runner) runHookCommand(ctx context.Context, kind string, hc schema.HookCommand, sharedDir string) *CommandFailure {
	cmd := ExpandSharedPlaceholders(hc.Cmd, sharedDir)
	r.Formatter.PrintHookCommand(kind, cmd)

	var extra []string
	for _, key := range sortedStringKeys(hc.Env) {
		extra = append(extra, key+"="+ExpandSharedPlaceholders(hc.Env[key], sharedDir))
	}
	env, added := r.commandEnv(extra...)

	var stdin string
	if hc.StdinFile != "" {
		data, err := os.ReadFile(resolveSource(hc.StdinFile, r.sourceDir))
		if err != nil {
			return &CommandFailure{Command: cmd, Detail: fmt.Sprintf("reading stdin_file: %v", err)}
		}
		stdin = string(data)
	}

	execResult, err := execute(ctx, execRequest{
		Cmd:         cmd,
		Stdin:       stdin,
		Env:         env,
		EnvExtra:    added,
		Timeout:     hc.EffectiveTimeout(),
		LowPriority: r.lowPriority,
		Sandbox:     r.plan,
	})
	if err != nil {
		return &CommandFailure{Command: cmd, Detail: fmt.Sprintf("execution: %v", err)}
	}
	if execResult.TimedOut {
		return &CommandFailure{
			Command: cmd,
			Detail:  fmt.Sprintf("command timed out after %s", hc.EffectiveTimeout()),
		}
	}
	if execResult.ExitCode != 0 {
		return &CommandFailure{
			Command: cmd,
			Detail:  fmt.Sprintf("exit code %d", execResult.ExitCode) + signalSuffix(execResult),
			Stdout:  execResult.Stdout,
			Stderr:  execResult.Stderr,
		}
	}
	return nil
}

// commandEnv builds the child environment for an executed command -- the one
// env-construction path shared by test commands and file-level setup/teardown
// commands. Execute replaces the child's environment entirely when given one,
// so a non-nil env always starts from os.Environ(); nil (no extra entries and
// no --coverdir) means plain inheritance. The extra entries are appended
// first and GOCOVERDIR last, so --coverdir wins even over an extra entry's
// own GOCOVERDIR value.
//
// added returns just the entries dats contributed, in the same order. A
// container sandbox starts the command from its image's environment rather
// than from ours, so those entries -- and only those -- are forwarded into
// it: the host's own PATH, HOME and the rest would be wrong inside.
func (r *Runner) commandEnv(extra ...string) (env []string, added []string) {
	if len(extra) == 0 && len(r.Env) == 0 && r.CoverDir == "" {
		return nil, nil
	}
	// Copied rather than appended in place: both slices belong to callers.
	// Runner.Env comes first so a test's own inputs.env wins over it.
	added = append(append([]string{}, r.Env...), extra...)
	if r.CoverDir != "" {
		added = append(added, "GOCOVERDIR="+r.CoverDir)
	}
	return append(os.Environ(), added...), added
}

// signalSuffix names the signal that killed the command, e.g.
// " (killed by signal: killed)"; empty for a normal exit. A signal death
// surfaces as exit code -1, so without the name the failure would be
// baffling.
func signalSuffix(execResult *ExecResult) string {
	if execResult.Signal == "" {
		return ""
	}
	return fmt.Sprintf(" (killed by signal: %s)", execResult.Signal)
}

// sourceDirOf returns the absolute directory holding the .dats file at path,
// which resolveSource in fixtures.go joins with a relative inputs.copy or
// shared.copy source. Absolute, rather than left relative to the process's
// current directory, so the resolution is stable even if that ever changes
// mid-run.
func sourceDirOf(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", path, err)
	}
	return filepath.Dir(abs), nil
}

// testName returns the display name for a test: its desc, falling back to
// the command.
func testName(test *schema.Test) string {
	if test.Desc != "" {
		return test.Desc
	}
	return test.Cmd
}

// instanceName returns the display name for an expanded test instance: the
// instance's desc-or-cmd name (both already matrix-substituted) with the
// matrix label appended, e.g. "greets [greeting=hello, name=alice]". The
// label appears whether or not the test has a desc.
func instanceName(instance *schema.TestInstance) string {
	name := testName(&instance.Test)
	if instance.Label != "" {
		name += " " + instance.Label
	}
	return name
}

// RunTest runs a single test. Canceling ctx kills the in-flight command's
// whole process group; the instance then fails fast with "execution: context
// canceled" or a signal death, never as a timeout.
func (r *Runner) RunTest(ctx context.Context, test *schema.Test, baseDir string, index int) TestResult {
	start := time.Now()

	result := TestResult{
		Name:  testName(test),
		Index: index,
	}

	// Setup fixtures
	fixtures, err := SetupFixtures(baseDir, index, test, r.sourceDir)
	if err != nil {
		result.Failures = append(result.Failures, fmt.Sprintf("fixture setup: %v", err))
		result.Duration = time.Since(start)
		return result
	}

	// Fixtures are built here and copied over, so the command finds them on
	// the machine it runs on. Setting RemoteBase first is what makes every
	// {inputs.X}/{outputs.X}/{shared.X} below expand to a remote path.
	if r.SSH != nil {
		fixtures.RemoteBase = r.remoteBase
		local := testDirPath(baseDir, index)
		if err := r.SSH.Push(ctx, local, fixtures.commandPath(local)); err != nil {
			result.Failures = append(result.Failures, err.Error())
			result.Duration = time.Since(start)
			return result
		}
	}

	// Expand placeholders in command
	cmd := ExpandPlaceholders(test.Cmd, fixtures)
	result.Command = cmd

	// Build environment for command execution: test env entries are appended
	// in sorted key order (deterministic), with values going through the same
	// placeholder expansion as the command.
	var extra []string
	for _, key := range sortedStringKeys(test.Inputs.Env) {
		extra = append(extra, key+"="+ExpandPlaceholders(test.Inputs.Env[key], fixtures))
	}
	env, added := r.commandEnv(extra...)

	// Execute the command
	execResult, err := execute(ctx, execRequest{
		Cmd:         cmd,
		Stdin:       test.Inputs.Stdin,
		Env:         env,
		EnvExtra:    added,
		Timeout:     test.Timeout.Value,
		LowPriority: r.lowPriority,
		Sandbox:     r.plan,
	})
	if err != nil {
		result.Failures = append(result.Failures, fmt.Sprintf("execution: %v", err))
		result.Duration = time.Since(start)
		return result
	}

	result.Stdout = execResult.Stdout
	result.Stderr = execResult.Stderr

	// Bring the outputs home before ANY assertion reads them. Unconditional:
	// a !files assertion ("must not exist") means nothing if the directory
	// was never collected.
	if r.SSH != nil {
		local := testDirPath(baseDir, index)
		if err := r.SSH.Pull(ctx, fixtures.commandPath(local), outputsDirName, local); err != nil {
			result.Failures = append(result.Failures, err.Error())
		}
	}

	// On timeout, report only the timeout and skip every other assertion:
	// checking the partial output or missing files would bury the real cause
	// under misleading secondary failures. (Stdout/stderr stay captured
	// above for verbose display.)
	if execResult.TimedOut {
		result.Failures = append(result.Failures, fmt.Sprintf("command timed out after %s", test.Timeout.Value))
		result.Duration = time.Since(start)
		return result
	}

	// Every early return above skipped this line, so snapshot assertions
	// (applied by the caller) run exactly when the command ran to
	// completion.
	result.ranToCompletion = true

	// Check exit code
	if err := AssertExitCode(execResult.ExitCode, test.Exit); err != nil {
		result.Failures = append(result.Failures, err.Error()+signalSuffix(execResult))
	}

	// Check stdout patterns
	for _, pattern := range test.Outputs.Stdout.Patterns {
		if err := AssertContains(execResult.Stdout, pattern); err != nil {
			result.Failures = append(result.Failures, fmt.Sprintf("stdout: %v", err))
		}
	}

	// Check stdout line-specific assertions
	if len(test.Outputs.Stdout.LineChecks) > 0 {
		lines := sortedKeys(test.Outputs.Stdout.LineChecks)
		for _, lineNum := range lines {
			pattern := test.Outputs.Stdout.LineChecks[lineNum]
			if err := AssertLineRegex(execResult.StdoutLines, lineNum, pattern); err != nil {
				result.Failures = append(result.Failures, fmt.Sprintf("stdout: %v", err))
			}
		}
	}

	// Check negated stdout patterns
	for _, pattern := range test.Outputs.NotStdout.Patterns {
		if err := RefuteContains(execResult.Stdout, pattern); err != nil {
			result.Failures = append(result.Failures, fmt.Sprintf("!stdout: %v", err))
		}
	}

	// Check negated stdout line-specific assertions
	if len(test.Outputs.NotStdout.LineChecks) > 0 {
		lines := sortedKeys(test.Outputs.NotStdout.LineChecks)
		for _, lineNum := range lines {
			pattern := test.Outputs.NotStdout.LineChecks[lineNum]
			if err := RefuteLineRegex(execResult.StdoutLines, lineNum, pattern); err != nil {
				result.Failures = append(result.Failures, fmt.Sprintf("!stdout: %v", err))
			}
		}
	}

	// Check stderr patterns
	for _, pattern := range test.Outputs.Stderr.Patterns {
		if err := AssertContains(execResult.Stderr, pattern); err != nil {
			result.Failures = append(result.Failures, fmt.Sprintf("stderr: %v", err))
		}
	}

	// Check stderr line-specific assertions
	if len(test.Outputs.Stderr.LineChecks) > 0 {
		lines := sortedKeys(test.Outputs.Stderr.LineChecks)
		for _, lineNum := range lines {
			pattern := test.Outputs.Stderr.LineChecks[lineNum]
			if err := AssertLineRegex(execResult.StderrLines, lineNum, pattern); err != nil {
				result.Failures = append(result.Failures, fmt.Sprintf("stderr: %v", err))
			}
		}
	}

	// Check negated stderr patterns
	for _, pattern := range test.Outputs.NotStderr.Patterns {
		if err := RefuteContains(execResult.Stderr, pattern); err != nil {
			result.Failures = append(result.Failures, fmt.Sprintf("!stderr: %v", err))
		}
	}

	// Check negated stderr line-specific assertions
	if len(test.Outputs.NotStderr.LineChecks) > 0 {
		lines := sortedKeys(test.Outputs.NotStderr.LineChecks)
		for _, lineNum := range lines {
			pattern := test.Outputs.NotStderr.LineChecks[lineNum]
			if err := RefuteLineRegex(execResult.StderrLines, lineNum, pattern); err != nil {
				result.Failures = append(result.Failures, fmt.Sprintf("!stderr: %v", err))
			}
		}
	}

	// Check the expected JSON value of stdout
	if test.Outputs.HasJSONOutput() {
		if expected, err := test.Outputs.JSONOutputValue(); err != nil {
			result.Failures = append(result.Failures, fmt.Sprintf("json_output: %v", err))
		} else {
			for _, err := range AssertJSONOutput(execResult.Stdout, expected) {
				result.Failures = append(result.Failures, err.Error())
			}
		}
	}

	// Check output files (files) and negated output files (!files). A !files
	// entry asserts the negation of each of its checks. Names are checked in
	// sorted order so failures report deterministically.
	for _, name := range sortedStringKeys(test.Outputs.Files) {
		result.Failures = append(result.Failures, checkFile("file "+name, outputPath(fixtures, baseDir, index, name), test.Outputs.Files[name], false)...)
	}
	for _, name := range sortedStringKeys(test.Outputs.NotFiles) {
		result.Failures = append(result.Failures, checkFile("!file "+name, outputPath(fixtures, baseDir, index, name), test.Outputs.NotFiles[name], true)...)
	}

	result.Passed = len(result.Failures) == 0
	result.Duration = time.Since(start)

	return result
}

// outputPath resolves the on-disk path for a named output file, falling back to
// the conventional location when the name was not pre-registered in the context.
// Non-local fallback names (traversal, absolute) are returned unchanged rather
// than joined, so they can never address a path outside the test directory.
func outputPath(ctx *TestContext, baseDir string, index int, name string) string {
	if path := ctx.OutputPaths[name]; path != "" {
		return path
	}
	if !filepath.IsLocal(name) {
		return name
	}
	return filepath.Join(baseDir, fmt.Sprintf("test-%d", index), outputsDirName, name)
}

// checkFile applies a FileCheck (exists/match/notMatch) at path and returns
// failure messages prefixed with label (e.g. "file out.txt" or "!file out.txt").
// With negate set (the !files form), every check is inverted: exists is
// flipped, match patterns must NOT match, and notMatch patterns must match.
// An empty check ({} or null) is an implicit existence assertion rather than a
// vacuous pass: under files the file must exist, under !files it must not.
func checkFile(label, path string, check schema.FileCheck, negate bool) []string {
	var failures []string
	if check.IsEmpty() {
		exists := true
		check.Exists = &exists
	}
	if check.Exists != nil {
		if *check.Exists != negate {
			if err := AssertFileExists(path); err != nil {
				failures = append(failures, fmt.Sprintf("%s: %v", label, err))
			}
		} else {
			if err := RefuteFileExists(path); err != nil {
				failures = append(failures, fmt.Sprintf("%s: %v", label, err))
			}
		}
	}
	assertContains, refuteContains := AssertFileContains, RefuteFileContains
	if negate {
		assertContains, refuteContains = RefuteFileContains, AssertFileContains
	}
	if len(check.Match) > 0 {
		for _, err := range assertContains(path, check.Match) {
			failures = append(failures, fmt.Sprintf("%s: %v", label, err))
		}
	}
	if len(check.NotMatch) > 0 {
		for _, err := range refuteContains(path, check.NotMatch) {
			failures = append(failures, fmt.Sprintf("%s: %v", label, err))
		}
	}
	return failures
}

// sortedKeys returns sorted keys from an int map
func sortedKeys(m map[int]string) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}
