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
	Update    bool
	// Sandbox selects the sandbox every command runs under.
	Sandbox *SandboxConfig

	Env []string

	plan *sandboxPlan

	sourceDir string

	// SSH picks which machine each file's commands run on; nil runs them here.
	SSH *SSHManager

	// ssh is the current file's connection, resolved by runFile beside plan.
	ssh *SSHConfig

	// refusedSSH is the file's own target when a typed one outranked it.
	refusedSSH string

	// remoteBase mirrors the current file's temp directory on the target.
	remoteBase string

	// datsPath is the file currently being run, for a per-test approval.
	datsPath string

	// altScopes holds the hosts tests overrode to; the file's target stays home.
	altMu     sync.Mutex
	altScopes map[string]*remoteScope

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

// RunFile runs all tests in a .dats file.
func (r *Runner) RunFile(ctx context.Context, path string) (*FileResult, error) {
	testFile, err := schema.ParseFile(path)
	if err != nil {
		return nil, err
	}
	return r.runFile(ctx, path, testFile, newSlots(runtime.NumCPU()))
}

// runFile runs one already-parsed file against a caller-provided pool.
func (r *Runner) runFile(ctx context.Context, path string, testFile *schema.TestFile, pool slots) (*FileResult, error) {
	// Create temp directory for fixtures
	tempDir, err := os.MkdirTemp("", "dats-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp directory: %w", err)
	}
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

	// Claim the file's directory on the ssh target before anything runs.
	r.datsPath = path
	if r.ssh, r.refusedSSH, err = r.SSH.Resolve(path, testFile.SSH); err != nil {
		return nil, err
	}
	defer r.closeAltScopes()
	if r.ssh != nil {
		if err := r.ssh.Connect(ctx); err != nil {
			return nil, err
		}
		if r.remoteBase, err = r.ssh.AllocBase(ctx); err != nil {
			return nil, err
		}
		if !r.KeepTemp {
			defer r.ssh.RemoveBase(r.remoteBase)
		} else {
			fmt.Fprintf(r.Formatter.Writer, "# Remote temp directory: %s:%s\n", r.ssh.Target, r.remoteBase)
		}
	}

	if r.plan, err = r.newSandboxPlan(testFile.Sandbox, tempDir); err != nil {
		return nil, err
	}
	if r.sourceDir, err = sourceDirOf(path); err != nil {
		return nil, err
	}

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

	sharedDir := filepath.Join(tempDir, sharedDirName)
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		return nil, fmt.Errorf("creating shared dir: %w", err)
	}

	// Write shared fixture files, then run setup commands in declared order, stopping at the first failure.
	if testFile.Shared != nil {
		if err := SetupSharedFixtures(sharedDir, testFile.Shared.Files, testFile.Shared.Copy, r.sourceDir); err != nil {
			result.SetupFailure = &CommandFailure{Detail: fmt.Sprintf("shared fixtures: %v", err)}
			r.Formatter.PrintHookFailure("setup", result.SetupFailure)
		}
	}
	// hookSharedDir is where a hook SEES shared/; the push is unconditional.
	hookSharedDir := sharedDir
	if r.ssh != nil && result.SetupFailure == nil {
		hookSharedDir = remoteJoin(r.remoteBase, sharedDirName)
		if err := r.ssh.Push(ctx, sharedDir, hookSharedDir); err != nil {
			result.SetupFailure = &CommandFailure{Detail: err.Error()}
			r.Formatter.PrintHookFailure("setup", result.SetupFailure)
		}
	}
	if result.SetupFailure == nil {
		for _, hc := range testFile.Setup {
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
		// Launch every instance; the pool bounds how many run at once.
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

	if r.Update && result.SetupFailure == nil {
		r.pruneStaleGoldens(result, instances, path)
		r.Formatter.PrintPrunedGoldens(result)
	}

	// Teardown always runs: in declared order, after test failures, and even when setup failed.
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

func (r *Runner) commandEnv(extra ...string) (env []string, added []string) {
	if len(extra) == 0 && len(r.Env) == 0 && r.CoverDir == "" {
		return nil, nil
	}
	// Copied rather than appended in place: both slices belong to callers.
	added = append(append([]string{}, r.Env...), extra...)
	if r.CoverDir != "" {
		added = append(added, "GOCOVERDIR="+r.CoverDir)
	}
	return append(os.Environ(), added...), added
}

func signalSuffix(execResult *ExecResult) string {
	if execResult.Signal == "" {
		return ""
	}
	return fmt.Sprintf(" (killed by signal: %s)", execResult.Signal)
}

func sourceDirOf(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", path, err)
	}
	return filepath.Dir(abs), nil
}

// testName returns the display name for a test: its desc, falling back to the command.
func testName(test *schema.Test) string {
	if test.Desc != "" {
		return test.Desc
	}
	return test.Cmd
}

func instanceName(instance *schema.TestInstance) string {
	name := testName(&instance.Test)
	if instance.Label != "" {
		name += " " + instance.Label
	}
	return name
}

// RunTest runs a single test.
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

	// Resolve where this instance runs: the file's target, or its override.
	sshCfg, remoteBase, err := r.scopeFor(ctx, test.SSH, filepath.Join(baseDir, sharedDirName))
	if err != nil {
		result.Failures = append(result.Failures, err.Error())
		result.Duration = time.Since(start)
		return result
	}
	plan := r.plan
	if sshCfg != nil && sshCfg != r.ssh {
		plan = plan.withSSH(sshCfg, remoteBase)
	}

	// Fixtures are built here and copied over, so the command finds them on the machine it runs on.
	if sshCfg != nil {
		fixtures.RemoteBase = remoteBase
		local := testDirPath(baseDir, index)
		if err := sshCfg.Push(ctx, local, fixtures.commandPath(local)); err != nil {
			result.Failures = append(result.Failures, err.Error())
			result.Duration = time.Since(start)
			return result
		}
	}

	// Expand placeholders in command
	cmd := ExpandPlaceholders(test.Cmd, fixtures)
	result.Command = cmd

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
		Sandbox:     plan,
	})
	if err != nil {
		result.Failures = append(result.Failures, fmt.Sprintf("execution: %v", err))
		result.Duration = time.Since(start)
		return result
	}

	result.Stdout = execResult.Stdout
	result.Stderr = execResult.Stderr

	// Bring the outputs home before ANY assertion reads them.
	if sshCfg != nil {
		local := testDirPath(baseDir, index)
		if err := sshCfg.Pull(ctx, fixtures.commandPath(local), outputsDirName, local); err != nil {
			result.Failures = append(result.Failures, err.Error())
		}
	}

	if execResult.TimedOut {
		result.Failures = append(result.Failures, fmt.Sprintf("command timed out after %s", test.Timeout.Value))
		result.Duration = time.Since(start)
		return result
	}

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

	// Check output files (files) and negated output files (!files).
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

func outputPath(ctx *TestContext, baseDir string, index int, name string) string {
	if path := ctx.OutputPaths[name]; path != "" {
		return path
	}
	if !filepath.IsLocal(name) {
		return name
	}
	return filepath.Join(baseDir, fmt.Sprintf("test-%d", index), outputsDirName, name)
}

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
