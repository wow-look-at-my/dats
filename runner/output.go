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
	UpdatedGoldens []string
	// Verbose output
	Command string
	Stdout  string
	Stderr  string

	ranToCompletion bool
}

// FileResult contains the results of running all tests in a file
type FileResult struct {
	Path    string
	Results []TestResult
	Passed  int
	Failed  int
	// SetupFailure records the shared-fixture write or setup command that failed before any test could run.
	SetupFailure *CommandFailure
	// TeardownFailures records every teardown command that failed.
	TeardownFailures []CommandFailure
	PrunedGoldens []string
}

func (fr *FileResult) Ok() bool {
	return fr.Failed == 0 && fr.SetupFailure == nil && len(fr.TeardownFailures) == 0
}

// CommandFailure describes a failed file-level setup or teardown step.
type CommandFailure struct {
	Command string
	// Detail says why it failed, e.g. "exit code 3" or "exit code -1 (killed by signal: killed)".
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

func (f *Formatter) PrintSandbox(desc, note string) {
	if desc == "" {
		return
	}
	fmt.Fprintf(f.Writer, "# sandbox: %s\n", desc)
	if note != "" {
		fmt.Fprintf(f.Writer, "# sandbox: %s\n", note)
	}
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

// PrintPrunedGoldens prints one line per stale snapshot golden file removed under --update.
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

func (f *Formatter) printCaptured(prefix, label, content string) {
	if content == "" {
		return
	}
	fmt.Fprintf(f.Writer, "%s%s:\n", prefix, label)
	for _, line := range strings.Split(strings.TrimSuffix(content, "\n"), "\n") {
		fmt.Fprintf(f.Writer, "%s  %s\n", prefix, line)
	}
}

// PrintHookCommand prints a file-level setup or teardown command as it is about to execute.
func (f *Formatter) PrintHookCommand(kind, cmd string) {
	if !f.Verbose {
		return
	}
	fmt.Fprintf(f.Writer, "# %s: %s\n", kind, cmd)
}

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

// PrintSummary prints the final summary.
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
