package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/wow-look-at-my/dats/schema"
)

// Runner executes tests from .dats files
type Runner struct {
	Verbose  bool
	KeepTemp bool   // Keep temp directory for debugging
	CoverDir string // Directory for GOCOVERDIR coverage data
	// Writable holds run-wide extra writable paths (the --writable flag): host
	// directories the CALLER owns and hands to the suites, which a .dats file
	// therefore cannot be expected to declare for itself. The build tool that
	// stages binaries into a directory and points suites at it is the one that
	// knows the path -- and knows whether those binaries need to write (a
	// self-modifying artifact such as an APE cannot even start read-only).
	Writable  []string
	Formatter *Formatter
	// Update rewrites snapshot golden files from actual output instead of
	// failing mismatches, and prunes stale goldens (the --update flag).
	Update bool
	// Sandbox selects the sandbox every command runs under (the --sandbox
	// flags). Nil -- the default for a Runner built directly, as library
	// callers do -- runs commands on the host; the CLI always sets one.
	// Backend detection inside it is memoized, so sharing one config across
	// files and workers probes the host at most once.
	Sandbox *SandboxConfig

	// plan is the resolved sandbox for the file currently being run, set by
	// RunFile/runFileParallel before any of that file's commands execute (nil
	// = run on the host). Per-file rather than per-run because the sandbox is
	// a file-level declaration; safe to hold on the Runner because a Runner
	// only ever runs one file at a time -- jobs mode gives each file its own.
	plan *sandboxPlan

	// lowPriority runs every spawned workload command -- test instances and
	// file-level setup/teardown hooks alike -- at low OS priority (unix nice
	// 19, best-effort; no-op on windows). Only the jobs-mode orchestration
	// (RunFilesParallel) sets it; serial runs never touch process priority.
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

	// Create temp directory for fixtures
	tempDir, err := os.MkdirTemp("", "dats-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp directory: %w", err)
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

	// Resolve the file's sandbox before anything runs: a file that must be
	// sandboxed and cannot be fails outright, rather than quietly running its
	// commands on the host.
	if r.plan, err = r.newSandboxPlan(testFile.Sandbox, tempDir); err != nil {
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

	r.Formatter.PrintSandbox(r.plan.describe())
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
		if err := SetupSharedFixtures(sharedDir, testFile.Shared.Files); err != nil {
			result.SetupFailure = &CommandFailure{Detail: fmt.Sprintf("shared fixtures: %v", err)}
			r.Formatter.PrintHookFailure("setup", result.SetupFailure)
		}
	}
	if result.SetupFailure == nil {
		for _, raw := range testFile.Setup {
			if fail := r.runHookCommand(ctx, "setup", raw, sharedDir); fail != nil {
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
		// Run each test instance; each gets its own test-<index> directory,
		// so identical fixture names across matrix instances never collide.
		// Snapshot assertions apply after the instance name is set -- the
		// golden file name derives from it.
		for i := range instances {
			testResult := r.RunTest(ctx, &instances[i].Test, tempDir, i)
			testResult.Name = instanceName(&instances[i])
			r.applySnapshot(&testResult, &instances[i], path, tempDir, i)
			result.Results = append(result.Results, testResult)
			r.Formatter.PrintResult(&testResult)

			if testResult.Passed {
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
	for _, raw := range testFile.Teardown {
		if fail := r.runHookCommand(teardownCtx, "teardown", raw, sharedDir); fail != nil {
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
// like test commands -- no stdin, no timeout), expanding only {shared.X}
// placeholders. It returns nil on success (exit 0) or the failure otherwise.
// Callers pass the live ctx for setup and a context.WithoutCancel ctx for
// teardown, which must run even after cancellation.
func (r *Runner) runHookCommand(ctx context.Context, kind, rawCmd, sharedDir string) *CommandFailure {
	cmd := ExpandSharedPlaceholders(rawCmd, sharedDir)
	r.Formatter.PrintHookCommand(kind, cmd)
	env, added := r.commandEnv()
	execResult, err := execute(ctx, execRequest{
		Cmd:         cmd,
		Env:         env,
		EnvExtra:    added,
		LowPriority: r.lowPriority,
		Sandbox:     r.plan,
	})
	if err != nil {
		return &CommandFailure{Command: cmd, Detail: fmt.Sprintf("execution: %v", err)}
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
	if len(extra) == 0 && r.CoverDir == "" {
		return nil, nil
	}
	added = extra
	if r.CoverDir != "" {
		// Copied rather than appended in place: extra belongs to the caller.
		added = append(append([]string{}, extra...), "GOCOVERDIR="+r.CoverDir)
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
	fixtures, err := SetupFixtures(baseDir, index, test)
	if err != nil {
		result.Failures = append(result.Failures, fmt.Sprintf("fixture setup: %v", err))
		result.Duration = time.Since(start)
		return result
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
	return filepath.Join(baseDir, fmt.Sprintf("test-%d", index), "outputs", name)
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
