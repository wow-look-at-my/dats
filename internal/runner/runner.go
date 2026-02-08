package runner

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/mhaynie/bats-declarative/internal/schema"
)

// Runner executes tests from .dats files
type Runner struct {
	Verbose   bool
	KeepTemp  bool // Keep temp directory for debugging
	Formatter *Formatter
}

// NewRunner creates a new test runner
func NewRunner(output io.Writer, verbose bool, keepTemp bool) *Runner {
	return &Runner{
		Verbose:  verbose,
		KeepTemp: keepTemp,
		Formatter: &Formatter{
			Writer:  output,
			Verbose: verbose,
		},
	}
}

// RunFile runs all tests in a .dats file
func (r *Runner) RunFile(path string) (*FileResult, error) {
	// Read and parse the input file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading input file: %w", err)
	}

	var testFile schema.TestFile
	if err := xml.Unmarshal(data, &testFile); err != nil {
		return nil, fmt.Errorf("parsing XML: %w", err)
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

	r.Formatter.PrintHeader(path, len(testFile.Tests))

	result := &FileResult{
		Path:    path,
		Results: make([]TestResult, 0, len(testFile.Tests)),
	}

	// Run each test
	for i, test := range testFile.Tests {
		testResult := r.RunTest(&test, tempDir, i)
		result.Results = append(result.Results, testResult)
		r.Formatter.PrintResult(&testResult)

		if testResult.Passed {
			result.Passed++
		} else {
			result.Failed++
		}
	}

	r.Formatter.PrintSummary(result)

	return result, nil
}

// RunTest runs a single test
func (r *Runner) RunTest(test *schema.Test, baseDir string, index int) TestResult {
	start := time.Now()

	// Determine test name
	name := test.Desc
	if name == "" {
		name = test.Cmd
	}

	result := TestResult{
		Name:  name,
		Index: index,
	}

	// Setup fixtures
	ctx, err := SetupFixtures(baseDir, index, test)
	if err != nil {
		result.Failures = append(result.Failures, fmt.Sprintf("fixture setup: %v", err))
		result.Duration = time.Since(start)
		return result
	}

	// Expand placeholders in command
	cmd := ExpandPlaceholders(test.Cmd, ctx)
	result.Command = cmd

	// Execute the command
	execResult, err := Execute(cmd, test.Stdin, nil)
	if err != nil {
		result.Failures = append(result.Failures, fmt.Sprintf("execution: %v", err))
		result.Duration = time.Since(start)
		return result
	}

	result.Stdout = execResult.Stdout
	result.Stderr = execResult.Stderr

	// Check exit code
	if err := AssertExitCode(execResult.ExitCode, test.Exit); err != nil {
		result.Failures = append(result.Failures, err.Error())
	}

	// Check stdout
	if test.Stdout != nil {
		for _, pattern := range test.Stdout.Match {
			if err := AssertContains(execResult.Stdout, pattern); err != nil {
				result.Failures = append(result.Failures, fmt.Sprintf("stdout: %v", err))
			}
		}

		for _, lineCheck := range test.Stdout.Lines {
			if err := AssertLineRegex(execResult.StdoutLines, lineCheck.N, lineCheck.Pattern); err != nil {
				result.Failures = append(result.Failures, fmt.Sprintf("stdout: %v", err))
			}
		}

		for _, pattern := range test.Stdout.NotMatch {
			if err := RefuteContains(execResult.Stdout, pattern); err != nil {
				result.Failures = append(result.Failures, fmt.Sprintf("!stdout: %v", err))
			}
		}
	}

	// Check stderr
	if test.Stderr != nil {
		for _, pattern := range test.Stderr.Match {
			if err := AssertContains(execResult.Stderr, pattern); err != nil {
				result.Failures = append(result.Failures, fmt.Sprintf("stderr: %v", err))
			}
		}

		for _, lineCheck := range test.Stderr.Lines {
			if err := AssertLineRegex(execResult.StderrLines, lineCheck.N, lineCheck.Pattern); err != nil {
				result.Failures = append(result.Failures, fmt.Sprintf("stderr: %v", err))
			}
		}

		for _, pattern := range test.Stderr.NotMatch {
			if err := RefuteContains(execResult.Stderr, pattern); err != nil {
				result.Failures = append(result.Failures, fmt.Sprintf("!stderr: %v", err))
			}
		}
	}

	// Check output files
	for _, output := range test.Outputs {
		path := ctx.OutputPaths[output.Name]
		if path == "" {
			// File wasn't in the outputs map, construct path
			path = fmt.Sprintf("%s/test-%d/outputs/%s", baseDir, index, output.Name)
		}

		if output.Exists.Set {
			if output.Exists.Value {
				if err := AssertFileExists(path); err != nil {
					result.Failures = append(result.Failures, fmt.Sprintf("file %s: %v", output.Name, err))
				}
			} else {
				if err := RefuteFileExists(path); err != nil {
					result.Failures = append(result.Failures, fmt.Sprintf("file %s: %v", output.Name, err))
				}
			}
		}

		if len(output.Match) > 0 {
			errs := AssertFileContains(path, output.Match)
			for _, err := range errs {
				result.Failures = append(result.Failures, fmt.Sprintf("file %s: %v", output.Name, err))
			}
		}

		if len(output.NotMatch) > 0 {
			errs := RefuteFileContains(path, output.NotMatch)
			for _, err := range errs {
				result.Failures = append(result.Failures, fmt.Sprintf("file %s: %v", output.Name, err))
			}
		}
	}

	result.Passed = len(result.Failures) == 0
	result.Duration = time.Since(start)

	return result
}
