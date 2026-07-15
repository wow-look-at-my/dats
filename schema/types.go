package schema

import (
	"fmt"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// exitCodeNames are the symbolic exit code names the runner can resolve.
// Only these parse; any other name could never pass a run.
var exitCodeNames = map[string]bool{
	"EXIT_SUCCESS": true, // 0
	"EXIT_FAILURE": true, // 1
}

// TestFile represents the root of a .dats file
type TestFile struct {
	// Schema optionally references the JSON Schema for IDE validation; the
	// runner ignores its value.
	Schema string `yaml:"$schema,omitempty"`
	Tests  []Test `yaml:"tests"`
}

// Test represents a single test case
type Test struct {
	Desc    string      `yaml:"desc,omitempty"`
	Exit    ExitCode    `yaml:"exit"`
	Cmd     string      `yaml:"cmd"`
	Timeout Duration    `yaml:"timeout,omitempty"`
	Inputs  InputBlock  `yaml:"inputs,omitempty"`
	Outputs OutputBlock `yaml:"outputs,omitempty"`
}

// InputBlock contains stdin, input files, and environment variables
type InputBlock struct {
	Stdin string            `yaml:"stdin,omitempty"`
	Files map[string]string `yaml:"files,omitempty"`
	// Env maps environment variable names to values. The variables are added
	// to the inherited environment for the test's command; values go through
	// the same placeholder expansion as the command.
	Env map[string]string `yaml:"env,omitempty"`
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
		if intVal < 0 || intVal > 255 {
			return fmt.Errorf("exit code %d must be in range 0-255", intVal)
		}
		e.Value = intVal
		return nil
	}
	// Try string - a quoted integer (e.g. "0") counts as its numeric value;
	// otherwise it must be a name the runner can resolve
	var strVal string
	if err := node.Decode(&strVal); err == nil {
		if intVal, err := strconv.Atoi(strVal); err == nil {
			if intVal < 0 || intVal > 255 {
				return fmt.Errorf("exit code %d must be in range 0-255", intVal)
			}
			e.Value = intVal
			return nil
		}
		if !exitCodeNames[strVal] {
			return fmt.Errorf("exit %q is not a recognized exit code name (use EXIT_SUCCESS, EXIT_FAILURE, or an integer 0-255)", strVal)
		}
		e.Variable = strVal
		return nil
	}
	return fmt.Errorf("exit must be an integer (0-255) or EXIT_SUCCESS/EXIT_FAILURE")
}

// Duration is a per-test timeout. It accepts either a bare integer number of
// seconds (e.g. 5) or a Go duration string (e.g. "500ms", "2s", "1m30s").
// A zero value means no timeout.
type Duration struct {
	Value time.Duration
}

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	// Bare integer = seconds
	var intVal int
	if err := node.Decode(&intVal); err == nil {
		if intVal < 0 {
			return fmt.Errorf("timeout %d must not be negative", intVal)
		}
		d.Value = time.Duration(intVal) * time.Second
		return nil
	}
	// String = Go duration; a quoted bare integer (e.g. "5") means seconds,
	// matching the unquoted form
	var strVal string
	if err := node.Decode(&strVal); err == nil {
		if intVal, err := strconv.Atoi(strVal); err == nil {
			if intVal < 0 {
				return fmt.Errorf("timeout %d must not be negative", intVal)
			}
			d.Value = time.Duration(intVal) * time.Second
			return nil
		}
		parsed, err := time.ParseDuration(strVal)
		if err != nil {
			return fmt.Errorf("invalid timeout %q: %w", strVal, err)
		}
		if parsed < 0 {
			return fmt.Errorf("timeout %q must not be negative", strVal)
		}
		d.Value = parsed
		return nil
	}
	return fmt.Errorf("timeout must be an integer (seconds) or duration string")
}

// OutputBlock contains all output validations
type OutputBlock struct {
	Stdout    OutputCheck          `yaml:"stdout,omitempty"`
	Stderr    OutputCheck          `yaml:"stderr,omitempty"`
	NotStdout OutputCheck          `yaml:"!stdout,omitempty"`
	NotStderr OutputCheck          `yaml:"!stderr,omitempty"`
	Files     map[string]FileCheck `yaml:"files,omitempty"`
	NotFiles  map[string]FileCheck `yaml:"!files,omitempty"`
	// JSONOutput is the expected JSON value of the whole stdout (json_output
	// key). Stored as a yaml.Node so an explicit null expectation is
	// distinguishable from an omitted key (zero node Kind).
	JSONOutput yaml.Node `yaml:"json_output,omitempty"`
}

// HasJSONOutput reports whether a json_output expectation was specified.
func (o *OutputBlock) HasJSONOutput() bool {
	return o.JSONOutput.Kind != 0
}

// JSONOutputValue decodes the json_output expectation into plain Go values
// (map[string]any, []any, string, bool, numbers, nil).
func (o *OutputBlock) JSONOutputValue() (any, error) {
	var v any
	if err := o.JSONOutput.Decode(&v); err != nil {
		return nil, fmt.Errorf("decoding json_output: %w", err)
	}
	return v, nil
}

// OutputCheck represents either:
// - A list of patterns to match anywhere in output
// - A map of line numbers to patterns (for line-specific assertions)
type OutputCheck struct {
	Patterns   []string       // patterns to match anywhere
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
			if lineNum < 0 {
				return fmt.Errorf("line number must be >= 0, got %d", lineNum)
			}
			// Iterating the mapping node directly bypasses yaml.v3's own
			// duplicate-key detection, so detect collisions here instead of
			// silently keeping the last entry.
			if _, exists := o.LineChecks[lineNum]; exists {
				return fmt.Errorf("duplicate line number %d in output check", lineNum)
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
	Match    []string `yaml:"match,omitempty"`
	NotMatch []string `yaml:"notMatch,omitempty"`
}

// IsEmpty reports whether the check asserts nothing explicitly (no exists,
// match, or notMatch). The runner treats an empty check as an implicit
// existence assertion: under files the file must exist, under !files it must
// not.
func (f FileCheck) IsEmpty() bool {
	return f.Exists == nil && len(f.Match) == 0 && len(f.NotMatch) == 0
}
