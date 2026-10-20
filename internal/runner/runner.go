package runner

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/wow-look-at-my/dats/internal/schema"
	datsrunner "github.com/wow-look-at-my/dats/runner"
)

// Runner executes tests from .dats files
type Runner struct {
	Verbose   bool
	KeepTemp  bool // Keep temp directory for debugging
	Formatter *datsrunner.Formatter
}

// NewRunner creates a new test runner
func NewRunner(output io.Writer, verbose bool, keepTemp bool) *Runner {
	return &Runner{
		Verbose:  verbose,
		KeepTemp: keepTemp,
		Formatter: &datsrunner.Formatter{
			Writer:  output,
			Verbose: verbose,
		},
	}
}

// RunFile runs all tests in a .dats file
func (r *Runner) RunFile(path string) (*datsrunner.FileResult, error) {
	// Read and parse the input file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading input file: %w", err)
	}

	var testFile schema.Dats
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

	r.Formatter.PrintHeader(path, len(testFile.Test))

	result := &datsrunner.FileResult{
		Path:    path,
		Results: make([]datsrunner.TestResult, 0, len(testFile.Test)),
	}

	// Run each test
	for i, test := range testFile.Test {
		testResult := r.RunTest(test, tempDir, i)
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
func (r *Runner) RunTest(test *schema.Test, baseDir string, index int) datsrunner.TestResult {
	start := time.Now()

	// Determine test name
	name := ""
	if test.DescAttr != nil {
		name = *test.DescAttr
	}
	if name == "" {
		name = test.CmdAttr
	}

	result := datsrunner.TestResult{
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
	cmd := ExpandPlaceholders(test.CmdAttr, ctx)
	result.Command = cmd

	// Execute the command
	stdin := ""
	if test.Stdin != nil {
		stdin = *test.Stdin
	}
	execResult, err := datsrunner.Execute(context.Background(), cmd, stdin, nil, 0)
	if err != nil {
		result.Failures = append(result.Failures, fmt.Sprintf("execution: %v", err))
		result.Duration = time.Since(start)
		return result
	}

	result.Stdout = execResult.Stdout
	result.Stderr = execResult.Stderr

	// Check exit code
	if err := assertExitCode(execResult.ExitCode, test.ExitAttr); err != nil {
		result.Failures = append(result.Failures, err.Error())
	}

	// Check stdout
	if test.Stdout != nil {
		for _, pattern := range test.Stdout.Match {
			if err := datsrunner.AssertContains(execResult.Stdout, pattern); err != nil {
				result.Failures = append(result.Failures, fmt.Sprintf("stdout: %v", err))
			}
		}

		for _, lineCheck := range test.Stdout.Line {
			if err := datsrunner.AssertLineRegex(execResult.StdoutLines, lineCheck.NAttr, lineCheck.Value); err != nil {
				result.Failures = append(result.Failures, fmt.Sprintf("stdout: %v", err))
			}
		}

		for _, pattern := range test.Stdout.NotMatch {
			if err := datsrunner.RefuteContains(execResult.Stdout, pattern); err != nil {
				result.Failures = append(result.Failures, fmt.Sprintf("!stdout: %v", err))
			}
		}
	}

	// Check stderr
	if test.Stderr != nil {
		for _, pattern := range test.Stderr.Match {
			if err := datsrunner.AssertContains(execResult.Stderr, pattern); err != nil {
				result.Failures = append(result.Failures, fmt.Sprintf("stderr: %v", err))
			}
		}

		for _, lineCheck := range test.Stderr.Line {
			if err := datsrunner.AssertLineRegex(execResult.StderrLines, lineCheck.NAttr, lineCheck.Value); err != nil {
				result.Failures = append(result.Failures, fmt.Sprintf("stderr: %v", err))
			}
		}

		for _, pattern := range test.Stderr.NotMatch {
			if err := datsrunner.RefuteContains(execResult.Stderr, pattern); err != nil {
				result.Failures = append(result.Failures, fmt.Sprintf("!stderr: %v", err))
			}
		}
	}

	// Check output files
	for _, output := range test.Output {
		path := ctx.OutputPaths[output.NameAttr]
		if path == "" {
			// File wasn't in the outputs map, construct path
			path = fmt.Sprintf("%s/test-%d/outputs/%s", baseDir, index, output.NameAttr)
		}

		if output.ExistsAttr != nil {
			if *output.ExistsAttr {
				if err := datsrunner.AssertFileExists(path); err != nil {
					result.Failures = append(result.Failures, fmt.Sprintf("file %s: %v", output.NameAttr, err))
				}
			} else {
				if err := datsrunner.RefuteFileExists(path); err != nil {
					result.Failures = append(result.Failures, fmt.Sprintf("file %s: %v", output.NameAttr, err))
				}
			}
		}

		if len(output.Match) > 0 {
			errs := datsrunner.AssertFileContains(path, output.Match)
			for _, err := range errs {
				result.Failures = append(result.Failures, fmt.Sprintf("file %s: %v", output.NameAttr, err))
			}
		}

		if len(output.NotMatch) > 0 {
			errs := datsrunner.RefuteFileContains(path, output.NotMatch)
			for _, err := range errs {
				result.Failures = append(result.Failures, fmt.Sprintf("file %s: %v", output.NameAttr, err))
			}
		}
	}

	result.Passed = len(result.Failures) == 0
	result.Duration = time.Since(start)

	return result
}

func assertExitCode(actual int, expected *schema.ExitCode) error {
	if expected == nil {
		if actual != 0 {
			return fmt.Errorf("expected exit code 0, got %d", actual)
		}
		return nil
	}

	expectedVal := expected.IntValue()
	if expected.IsVariable() {
		switch expected.VariableName() {
		case "EXIT_SUCCESS":
			expectedVal = 0
		case "EXIT_FAILURE":
			expectedVal = 1
		default:
			return fmt.Errorf("unknown exit code variable: %s", expected.VariableName())
		}
	}

	if actual != expectedVal {
		return fmt.Errorf("expected exit code %d, got %d", expectedVal, actual)
	}
	return nil
}
