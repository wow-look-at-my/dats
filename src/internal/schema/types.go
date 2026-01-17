package schema

import (
	"fmt"
	"strconv"

	"gopkg.in/yaml.v3"
)

// TestFile represents the root of a .dats file
type TestFile struct {
	Tests []Test `yaml:"tests"`
}

// Test represents a single test case
type Test struct {
	Name    string            `yaml:"name"`
	Exit    ExitCode          `yaml:"exit"`
	Cmd     string            `yaml:"cmd"`
	Stdin   string            `yaml:"stdin,omitempty"`
	Inputs  map[string]string `yaml:"inputs,omitempty"`
	Outputs OutputBlock       `yaml:"outputs,omitempty"`
}

// ExitCode can be an int or a string like "EXIT_SUCCESS"
type ExitCode struct {
	Value    int
	Variable string // If non-empty, use this variable name instead of Value
}

func (e *ExitCode) UnmarshalYAML(node *yaml.Node) error {
	// Try int first
	var intVal int
	if err := node.Decode(&intVal); err == nil {
		e.Value = intVal
		return nil
	}
	// Try string
	var strVal string
	if err := node.Decode(&strVal); err == nil {
		e.Variable = strVal
		return nil
	}
	return fmt.Errorf("exit must be an integer or EXIT_* variable name")
}

// String returns the exit code as a string for BATS assertions
func (e ExitCode) String() string {
	if e.Variable != "" {
		return "$" + e.Variable
	}
	return strconv.Itoa(e.Value)
}

// OutputBlock contains all output validations
type OutputBlock struct {
	Stdout    OutputCheck          // stdout patterns or line checks
	Stderr    OutputCheck          // stderr patterns or line checks
	NotStdout OutputCheck          // negative stdout assertions (!stdout)
	NotStderr OutputCheck          // negative stderr assertions (!stderr)
	Files     map[string]FileCheck // output files to validate
}

func (o *OutputBlock) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("outputs must be a mapping")
	}

	o.Files = make(map[string]FileCheck)

	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]

		key := keyNode.Value

		switch key {
		case "stdout":
			var check OutputCheck
			if err := valueNode.Decode(&check); err != nil {
				return fmt.Errorf("invalid stdout: %w", err)
			}
			o.Stdout = check
		case "stderr":
			var check OutputCheck
			if err := valueNode.Decode(&check); err != nil {
				return fmt.Errorf("invalid stderr: %w", err)
			}
			o.Stderr = check
		case "!stdout":
			var check OutputCheck
			if err := valueNode.Decode(&check); err != nil {
				return fmt.Errorf("invalid !stdout: %w", err)
			}
			o.NotStdout = check
		case "!stderr":
			var check OutputCheck
			if err := valueNode.Decode(&check); err != nil {
				return fmt.Errorf("invalid !stderr: %w", err)
			}
			o.NotStderr = check
		default:
			// It's a file check
			var fileCheck FileCheck
			if err := valueNode.Decode(&fileCheck); err != nil {
				return fmt.Errorf("invalid file check for %s: %w", key, err)
			}
			o.Files[key] = fileCheck
		}
	}

	return nil
}

// OutputCheck represents either:
// - A list of patterns to match anywhere in output
// - A map of line numbers to patterns (for line-specific assertions)
type OutputCheck struct {
	Patterns  []string       // patterns to match anywhere
	LineChecks map[int]string // line-specific patterns (0-indexed)
}

func (o *OutputCheck) UnmarshalYAML(node *yaml.Node) error {
	// Try sequence first (list of patterns)
	if node.Kind == yaml.SequenceNode {
		var patterns []string
		if err := node.Decode(&patterns); err != nil {
			return err
		}
		o.Patterns = patterns
		return nil
	}

	// Try mapping (line-specific checks)
	if node.Kind == yaml.MappingNode {
		o.LineChecks = make(map[int]string)
		for i := 0; i < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			valueNode := node.Content[i+1]

			// Parse key as int
			lineNum, err := strconv.Atoi(keyNode.Value)
			if err != nil {
				return fmt.Errorf("line check key must be an integer, got %q", keyNode.Value)
			}

			var pattern string
			if err := valueNode.Decode(&pattern); err != nil {
				return err
			}
			o.LineChecks[lineNum] = pattern
		}
		return nil
	}

	return fmt.Errorf("output check must be a list of patterns or map of line checks")
}

// IsEmpty returns true if no checks are defined
func (o OutputCheck) IsEmpty() bool {
	return len(o.Patterns) == 0 && len(o.LineChecks) == 0
}

// FileCheck defines validation for an output file
type FileCheck struct {
	Exists   *bool    `yaml:"exists,omitempty"`
	Contains []string `yaml:"contains,omitempty"`
}
