package runner

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/wow-look-at-my/dats/schema"
)

// AssertContains checks if pattern appears as a substring in text
func AssertContains(text, pattern string) error {
	if !strings.Contains(text, pattern) {
		return fmt.Errorf("expected output to contain %q", pattern)
	}
	return nil
}

// RefuteContains checks that pattern does NOT appear in text
func RefuteContains(text, pattern string) error {
	if strings.Contains(text, pattern) {
		return fmt.Errorf("expected output to NOT contain %q", pattern)
	}
	return nil
}

// AssertLineRegex checks if line N matches the given regex pattern
func AssertLineRegex(lines []string, lineNum int, pattern string) error {
	if lineNum < 0 || lineNum >= len(lines) {
		return fmt.Errorf("line %d does not exist (output has %d lines)", lineNum, len(lines))
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("invalid regex %q: %w", pattern, err)
	}

	if !re.MatchString(lines[lineNum]) {
		return fmt.Errorf("line %d: expected to match %q, got %q", lineNum, pattern, lines[lineNum])
	}
	return nil
}

// AssertExitCode checks if the actual exit code matches expected
func AssertExitCode(actual int, expected schema.ExitCode) error {
	// If using a variable, we need to resolve it
	expectedVal := expected.Value
	if expected.Variable != "" {
		// For EXIT_SUCCESS and EXIT_FAILURE, use standard values
		switch expected.Variable {
		case "EXIT_SUCCESS":
			expectedVal = 0
		case "EXIT_FAILURE":
			expectedVal = 1
		default:
			// Unknown variable - we can't resolve it without bash context
			return fmt.Errorf("unknown exit code variable: %s", expected.Variable)
		}
	}

	if actual != expectedVal {
		return fmt.Errorf("expected exit code %d, got %d", expectedVal, actual)
	}
	return nil
}

// AssertFileExists checks that a file exists
func AssertFileExists(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("expected file %q to exist", path)
	}
	return nil
}

// RefuteFileExists checks that a file does NOT exist
func RefuteFileExists(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("expected file %q to NOT exist", path)
	}
	return nil
}

// AssertFileContains checks if file contains all given patterns
func AssertFileContains(path string, patterns []string) []error {
	content, err := os.ReadFile(path)
	if err != nil {
		return []error{fmt.Errorf("could not read file %q: %w", path, err)}
	}

	text := string(content)
	var errors []error
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			errors = append(errors, fmt.Errorf("invalid regex %q: %w", pattern, err))
			continue
		}
		if !re.MatchString(text) {
			errors = append(errors, fmt.Errorf("file %q: expected to match %q", path, pattern))
		}
	}
	return errors
}

// RefuteFileContains checks that file does NOT contain any of the given patterns
func RefuteFileContains(path string, patterns []string) []error {
	content, err := os.ReadFile(path)
	if err != nil {
		// File doesn't exist - that's fine for refute
		return nil
	}

	text := string(content)
	var errors []error
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			errors = append(errors, fmt.Errorf("invalid regex %q: %w", pattern, err))
			continue
		}
		if re.MatchString(text) {
			errors = append(errors, fmt.Errorf("file %q: expected to NOT match %q", path, pattern))
		}
	}
	return errors
}
