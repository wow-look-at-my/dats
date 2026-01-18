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
	// Verbose output
	Command string
	Stdout  string
	Stderr  string
}

// FileResult contains the results of running all tests in a file
type FileResult struct {
	Path    string
	Results []TestResult
	Passed  int
	Failed  int
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

	if f.Verbose {
		f.printVerboseDetails(r)
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
		if r.Stdout != "" {
			fmt.Fprintf(f.Writer, "  # stdout:\n")
			for _, line := range strings.Split(strings.TrimSuffix(r.Stdout, "\n"), "\n") {
				fmt.Fprintf(f.Writer, "  #   %s\n", line)
			}
		}
		if r.Stderr != "" {
			fmt.Fprintf(f.Writer, "  # stderr:\n")
			for _, line := range strings.Split(strings.TrimSuffix(r.Stderr, "\n"), "\n") {
				fmt.Fprintf(f.Writer, "  #   %s\n", line)
			}
		}
	}
}

// PrintSummary prints the final summary
func (f *Formatter) PrintSummary(fr *FileResult) {
	fmt.Fprintf(f.Writer, "\n%d/%d passed", fr.Passed, fr.Passed+fr.Failed)
	if fr.Failed > 0 {
		fmt.Fprintf(f.Writer, ", %d failed", fr.Failed)
	}
	fmt.Fprintf(f.Writer, "\n")
}

// PrintError prints an error message
func (f *Formatter) PrintError(format string, args ...interface{}) {
	fmt.Fprintf(f.Writer, "Error: "+format+"\n", args...)
}
