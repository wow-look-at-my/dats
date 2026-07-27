package schema

import (
	"fmt"
	"strconv"
	"strings"
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
	// Setup commands run once, in declared order, before any of the file's
	// tests (after shared files are written). If one fails, the remaining
	// setup commands are skipped and every test in the file is reported as
	// failed.
	Setup SetupCommands `yaml:"setup,omitempty"`
	// Teardown commands always run once, in declared order, after the file's
	// tests -- after test failures and even when setup failed. A failing
	// teardown command does not stop the rest, but marks the file failed.
	Teardown TeardownCommands `yaml:"teardown,omitempty"`
	// Shared declares fixture files written once per file into the shared
	// directory; nil when the file has no shared block.
	Shared *Shared `yaml:"shared,omitempty"`
	// Sandbox refines (or opts out of) the sandbox the CLI selected for this
	// file's commands; nil when the file has no sandbox block, leaving every
	// decision to the CLI.
	Sandbox *SandboxSpec `yaml:"sandbox,omitempty"`
	Tests   []Test       `yaml:"tests"`
}

// CommandList is an ordered list of shell commands: the value shape of the
// file-level setup and teardown keys. In YAML it is written as either a
// single scalar string or a sequence of scalar strings. SetupCommands and
// TeardownCommands wrap it so parse errors can name their key, mirroring how
// ExitCode and Duration hardcode "exit" and "timeout" in their messages.
type CommandList []string

// SetupCommands is the file-level setup key: commands run once, in order,
// before any of the file's tests.
type SetupCommands CommandList

func (s *SetupCommands) UnmarshalYAML(node *yaml.Node) error {
	cmds, err := unmarshalCommandList(node, "setup")
	if err != nil {
		return err
	}
	*s = SetupCommands(cmds)
	return nil
}

// TeardownCommands is the file-level teardown key: commands that always run
// once, in order, after the file's tests.
type TeardownCommands CommandList

func (td *TeardownCommands) UnmarshalYAML(node *yaml.Node) error {
	cmds, err := unmarshalCommandList(node, "teardown")
	if err != nil {
		return err
	}
	*td = TeardownCommands(cmds)
	return nil
}

// unmarshalCommandList decodes the two accepted CommandList shapes, naming
// key (setup or teardown) in every error.
func unmarshalCommandList(node *yaml.Node, key string) (CommandList, error) {
	switch node.Kind {
	case yaml.ScalarNode:
		cmd, err := commandFromNode(node, key, "command")
		if err != nil {
			return nil, err
		}
		return CommandList{cmd}, nil
	case yaml.SequenceNode:
		if len(node.Content) == 0 {
			return nil, fmt.Errorf("%s: must list at least one command", key)
		}
		cmds := make(CommandList, 0, len(node.Content))
		for i, item := range node.Content {
			cmd, err := commandFromNode(item, key, fmt.Sprintf("command %d", i+1))
			if err != nil {
				return nil, err
			}
			cmds = append(cmds, cmd)
		}
		return cmds, nil
	}
	return nil, fmt.Errorf("%s must be a command string or a list of command strings", key)
}

// commandFromNode validates one command scalar, naming key (setup or
// teardown) and label ("command" or "command N") in every error. Only true
// strings are commands: yaml.v3 would happily coerce a bare 42 into "42",
// which could never be a meaningful command.
func commandFromNode(node *yaml.Node, key, label string) (string, error) {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return "", fmt.Errorf("%s: %s must be a string", key, label)
	}
	if strings.TrimSpace(node.Value) == "" {
		return "", fmt.Errorf("%s: %s must not be empty", key, label)
	}
	return node.Value, nil
}

// Shared declares file-level fixture files, written once into the file's
// shared directory before setup runs. Tests address them with {shared.X}
// placeholders and should treat them as read-only.
type Shared struct {
	// Files maps file name to content. Contents go through {shared.X}
	// placeholder expansion only; names must be local relative paths.
	Files map[string]string `yaml:"files"`
}

// Test represents a single test case
type Test struct {
	Desc    string   `yaml:"desc,omitempty"`
	Exit    ExitCode `yaml:"exit"`
	Cmd     string   `yaml:"cmd"`
	Timeout Duration `yaml:"timeout,omitempty"`
	// Matrix declares the test's parameter variables; when non-empty, the
	// test expands into one instance per combination of values (see
	// ExpandMatrix). Nil for ordinary tests.
	Matrix  Matrix      `yaml:"matrix,omitempty"`
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
	// Reject floats explicitly (even integral ones like 2.0): yaml.v3 would
	// otherwise silently truncate them in the int decode below, and
	// schema.json types exit as an integer.
	if node.Tag == "!!float" {
		return fmt.Errorf("exit code must be an integer in range 0-255, got float %s", node.Value)
	}
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
	// Reject floats explicitly (even integral ones like 1.0): yaml.v3 would
	// otherwise silently truncate them in the int decode below, turning
	// timeout: 0.9 into 0 seconds -- i.e. no timeout at all. Fractional
	// seconds are expressed as a duration string instead.
	if node.Tag == "!!float" {
		return fmt.Errorf("timeout must be an integer number of seconds or a duration string (e.g. \"900ms\", \"1.5s\"), got float %s", node.Value)
	}
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
	// Snapshot configures golden-file assertions for the test's output
	// streams (snapshot key). A value type so matrix expansion's copyTest
	// duplicates it by plain value copy.
	Snapshot SnapshotCheck `yaml:"snapshot,omitempty"`
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

// SnapshotCheck configures golden-file (snapshot) assertions for a test's
// output streams. Zero value = no snapshot assertion.
type SnapshotCheck struct {
	// Enabled distinguishes a present snapshot key from an absent one:
	// `snapshot: false` and an omitted key are both the zero value.
	Enabled bool
	Stdout  bool
	Stderr  bool
}

// UnmarshalYAML decodes the two accepted snapshot shapes: a scalar boolean
// (`snapshot: true` snapshots stdout; `snapshot: false` is the documented
// toggle-off, identical to omitting the key) or a mapping of stream names
// (stdout, stderr) to booleans, of which at least one must be true. The
// mapping node is iterated directly, which bypasses yaml.v3's own
// duplicate-key detection, so duplicates are detected here, mirroring
// OutputCheck's line-map handling.
func (s *SnapshotCheck) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		var enabled bool
		if err := node.Decode(&enabled); err != nil {
			return fmt.Errorf("snapshot: must be true, false, or a mapping of stream booleans (stdout, stderr)")
		}
		if enabled {
			*s = SnapshotCheck{Enabled: true, Stdout: true}
		} else {
			*s = SnapshotCheck{}
		}
		return nil
	case yaml.MappingNode:
		check := SnapshotCheck{}
		seen := make(map[string]bool, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			keyNode, valNode := node.Content[i], node.Content[i+1]
			key := keyNode.Value
			if key != "stdout" && key != "stderr" {
				return fmt.Errorf("snapshot: unknown key %q (allowed: stdout, stderr)", key)
			}
			if seen[key] {
				return fmt.Errorf("snapshot: %s declared more than once", key)
			}
			seen[key] = true
			var enabled bool
			if valNode.Kind != yaml.ScalarNode || valNode.Decode(&enabled) != nil {
				return fmt.Errorf("snapshot: %s must be a boolean", key)
			}
			if key == "stdout" {
				check.Stdout = enabled
			} else {
				check.Stderr = enabled
			}
		}
		// A mapping that enables nothing (empty, or explicit falses) can only
		// be a mistake: it looks like an assertion but asserts nothing.
		if !check.Stdout && !check.Stderr {
			return fmt.Errorf("snapshot: must enable at least one of stdout, stderr")
		}
		check.Enabled = true
		*s = check
		return nil
	}
	return fmt.Errorf("snapshot: must be true, false, or a mapping of stream booleans (stdout, stderr)")
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
