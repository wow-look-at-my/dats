package runner

import (
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/mhaynie/bats-declarative/schema"
	"gopkg.in/yaml.v3"
)

// Runner executes tests from .dats files
type Runner struct {
	Verbose   bool
	KeepTemp  bool   // Keep temp directory for debugging
	CoverDir  string // Directory for GOCOVERDIR coverage data
	Formatter *Formatter
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

// RunFile runs all tests in a .dats file
func (r *Runner) RunFile(path string) (*FileResult, error) {
	// Read and parse the input file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading input file: %w", err)
	}

	var testFile schema.TestFile
	if err := yaml.Unmarshal(data, &testFile); err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
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

	// Build environment for command execution
	var env []string
	if r.CoverDir != "" {
		env = append(os.Environ(), "GOCOVERDIR="+r.CoverDir)
	}

	// Execute the command
	execResult, err := Execute(cmd, test.Inputs.Stdin, env)
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

	// Check output files
	for name, check := range test.Outputs.Files {
		path := ctx.OutputPaths[name]
		if path == "" {
			// File wasn't in the outputs map, construct path
			path = fmt.Sprintf("%s/test-%d/outputs/%s", baseDir, index, name)
		}

		if check.Exists != nil {
			if *check.Exists {
				if err := AssertFileExists(path); err != nil {
					result.Failures = append(result.Failures, fmt.Sprintf("file %s: %v", name, err))
				}
			} else {
				if err := RefuteFileExists(path); err != nil {
					result.Failures = append(result.Failures, fmt.Sprintf("file %s: %v", name, err))
				}
			}
		}

		if len(check.Match) > 0 {
			errs := AssertFileContains(path, check.Match)
			for _, err := range errs {
				result.Failures = append(result.Failures, fmt.Sprintf("file %s: %v", name, err))
			}
		}

		if len(check.NotMatch) > 0 {
			errs := RefuteFileContains(path, check.NotMatch)
			for _, err := range errs {
				result.Failures = append(result.Failures, fmt.Sprintf("file %s: %v", name, err))
			}
		}
	}

	// Check negated output files (!files)
	for name, check := range test.Outputs.NotFiles {
		path := fmt.Sprintf("%s/test-%d/outputs/%s", baseDir, index, name)

		if check.Exists != nil {
			if *check.Exists {
				if err := AssertFileExists(path); err != nil {
					result.Failures = append(result.Failures, fmt.Sprintf("!file %s: %v", name, err))
				}
			} else {
				if err := RefuteFileExists(path); err != nil {
					result.Failures = append(result.Failures, fmt.Sprintf("!file %s: %v", name, err))
				}
			}
		}
	}

	result.Passed = len(result.Failures) == 0
	result.Duration = time.Since(start)

	return result
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
