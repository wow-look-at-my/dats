package runner

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// TestResult contains the result of running a single test
type TestResult struct {
	Name     string
	Index    int
	Passed   bool
	Duration time.Duration
	Failures []string
	// UpdatedGoldens lists the snapshot golden files rewritten from this
	// instance's actual output under --update (missing or differing goldens
	// only; up-to-date goldens are never rewritten or listed).
	UpdatedGoldens []string
	// Verbose output
	Command string
	Stdout  string
	Stderr  string

	// ranToCompletion is true when the command actually ran and exited on
	// its own (not a fixture-setup failure, spawn failure, or timeout).
	// Snapshot assertions apply only to completed runs, mirroring how a
	// timeout skips every other assertion.
	ranToCompletion bool
}

// FileResult contains the results of running all tests in a file
type FileResult struct {
	Path    string
	Results []TestResult
	Passed  int
	Failed  int
	// SetupFailure records the shared-fixture write or setup command that
	// failed before any test could run. When set, every test in the file is
	// reported as a failure (never silently skipped).
	SetupFailure *CommandFailure
	// TeardownFailures records every teardown command that failed. Teardown
	// always runs -- after test failures and even when setup failed -- and
	// any entry here marks the whole file failed, even when every test
	// passed.
	TeardownFailures []CommandFailure
	// PrunedGoldens lists the stale snapshot golden files removed from the
	// file's snapshot directory under --update (sorted). Empty on ordinary
	// runs.
	PrunedGoldens []string
}

// Ok reports whether the file run passed as a whole: every test passed, setup
// succeeded, and no teardown command failed.
func (fr *FileResult) Ok() bool {
	return fr.Failed == 0 && fr.SetupFailure == nil && len(fr.TeardownFailures) == 0
}

// CommandFailure describes a failed file-level setup or teardown step.
type CommandFailure struct {
	// Command is the command as executed (after {shared.X} expansion), or ""
	// when the failure was not a command (e.g. writing shared fixtures).
	Command string
	// Detail says why it failed, e.g. "exit code 3" or
	// "exit code -1 (killed by signal: killed)".
	Detail string
	Stdout string // captured stdout of the failed command
	Stderr string // captured stderr of the failed command
}

// Formatter handles output formatting
type Formatter struct {
	Writer  io.Writer
	Verbose bool
}

// PrintHeader prints the file header
func (f *Formatter) PrintHeader(path string, testCount int) {
	fmt.Fprintf(f.Writer, "Running %s (%d tests)\n\n", path, testCount)
}

// PrintSandbox announces, ahead of a file's header, the sandbox that file's
// commands run under -- desc comes from the resolved plan and is empty when
// the commands run directly on the host, so an unsandboxed run's output stays
// byte-for-byte what it has always been.
func (f *Formatter) PrintSandbox(desc string) {
	if desc == "" {
		return
	}
	fmt.Fprintf(f.Writer, "# sandbox: %s\n", desc)
}

// PrintResult prints a single test result
func (f *Formatter) PrintResult(r *TestResult) {
	if r.Passed {
		fmt.Fprintf(f.Writer, "ok %d - %s\n", r.Index+1, r.Name)
	} else {
		fmt.Fprintf(f.Writer, "not ok %d - %s\n", r.Index+1, r.Name)
		for _, failure := range r.Failures {
			fmt.Fprintf(f.Writer, "  # %s\n", failure)
		}
	}

	for _, path := range r.UpdatedGoldens {
		fmt.Fprintf(f.Writer, "  # updated golden: %s\n", path)
	}

	if f.Verbose {
		f.printVerboseDetails(r)
	}
}

// PrintPrunedGoldens prints one line per stale snapshot golden file removed
// under --update. Nothing is printed on ordinary runs (the list is empty).
func (f *Formatter) PrintPrunedGoldens(fr *FileResult) {
	for _, path := range fr.PrunedGoldens {
		fmt.Fprintf(f.Writer, "# pruned stale golden: %s\n", path)
	}
}

func (f *Formatter) printVerboseDetails(r *TestResult) {
	if r.Command != "" {
		fmt.Fprintf(f.Writer, "  # command: %s\n", r.Command)
	}
	if r.Duration > 0 {
		fmt.Fprintf(f.Writer, "  # duration: %s\n", r.Duration)
	}
	if !r.Passed {
		f.printCaptured("  # ", "stdout", r.Stdout)
		f.printCaptured("  # ", "stderr", r.Stderr)
	}
}

// printCaptured renders a captured output stream under a "label:" line, each
// line indented two spaces past prefix. Empty output prints nothing.
func (f *Formatter) printCaptured(prefix, label, content string) {
	if content == "" {
		return
	}
	fmt.Fprintf(f.Writer, "%s%s:\n", prefix, label)
	for _, line := range strings.Split(strings.TrimSuffix(content, "\n"), "\n") {
		fmt.Fprintf(f.Writer, "%s  %s\n", prefix, line)
	}
}

// PrintHookCommand prints a file-level setup or teardown command as it is
// about to execute. Verbose only, mirroring how -v shows test commands.
func (f *Formatter) PrintHookCommand(kind, cmd string) {
	if !f.Verbose {
		return
	}
	fmt.Fprintf(f.Writer, "# %s: %s\n", kind, cmd)
}

// PrintHookFailure prints the loud file-level diagnostic for a failed setup
// or teardown step: the command (when there is one), why it failed, and its
// captured output rendered the way failing-test output is rendered.
func (f *Formatter) PrintHookFailure(kind string, fail *CommandFailure) {
	if fail.Command != "" {
		fmt.Fprintf(f.Writer, "# %s command failed: %s\n", kind, fail.Command)
		fmt.Fprintf(f.Writer, "#   %s\n", fail.Detail)
	} else {
		fmt.Fprintf(f.Writer, "# %s failed: %s\n", kind, fail.Detail)
	}
	f.printCaptured("#   ", "stdout", fail.Stdout)
	f.printCaptured("#   ", "stderr", fail.Stderr)
}

// PrintSummary prints the final summary. Test counts stay test counts; a
// teardown failure is called out separately so "N/N passed" can never read as
// a clean run when the file still failed.
func (f *Formatter) PrintSummary(fr *FileResult) {
	fmt.Fprintf(f.Writer, "\n%d/%d passed", fr.Passed, fr.Passed+fr.Failed)
	if fr.Failed > 0 {
		fmt.Fprintf(f.Writer, ", %d failed", fr.Failed)
	}
	if len(fr.TeardownFailures) > 0 {
		fmt.Fprintf(f.Writer, ", teardown failed")
	}
	fmt.Fprintf(f.Writer, "\n")
}

// PrintError prints an error message
func (f *Formatter) PrintError(format string, args ...interface{}) {
	fmt.Fprintf(f.Writer, "Error: "+format+"\n", args...)
}
