package schema

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/wow-look-at-my/go-containers/set"
	yamlfixed "github.com/wow-look-at-my/yaml-fixed/yaml"
)

// exitCodeNames are the symbolic exit code names the runner can resolve.
// Only these parse; any other name could never pass a run.
var exitCodeNames = set.Of(
	"EXIT_SUCCESS", // 0
	"EXIT_FAILURE", // 1
)

// TestFile represents the root of a .dats file
type TestFile struct {
	// Schema names the JSON Schema for IDE validation; the runner ignores it.
	Schema string `yaml:"$schema,omitempty"`
	// Setup runs once before the tests; a failure fails every test in the file.
	Setup SetupCommands `yaml:"setup,omitempty"`
	// Teardown always runs after the tests, and a failure fails the file.
	Teardown TeardownCommands `yaml:"teardown,omitempty"`
	// Shared declares fixtures written once per file; nil when there is none.
	Shared *Shared `yaml:"shared,omitempty"`
	// Sandbox narrows the CLI's sandbox for this file; nil narrows nothing.
	Sandbox *SandboxSpec `yaml:"sandbox,omitempty"`
	Tests   []Test       `yaml:"tests"`
}

// DefaultHookTimeout bounds a hook that states none: unlike a test, a hook
// always has a bound, since an unbounded one hangs the run silently.
const DefaultHookTimeout = 30 * time.Second

// HookCommand is one setup/teardown list entry: a command plus the settings
// that shape how it runs. In YAML it is written as either a bare command
// string (no extra env, no stdin, DefaultHookTimeout) or a mapping with cmd
// plus optional env, stdin_file, and timeout.
type HookCommand struct {
	Cmd string
	// Env adds entries on top of the inherited environment; {shared.X} only.
	Env map[string]string
	// StdinFile pipes a host file's raw content in, resolved like inputs.copy.
	StdinFile string
	// Timeout is this entry's bound; nil means DefaultHookTimeout, and an
	// explicit value must be > 0. A hook cannot spell "no timeout".
	Timeout *Duration
}

// EffectiveTimeout is the entry's own Timeout, else DefaultHookTimeout.
func (h HookCommand) EffectiveTimeout() time.Duration {
	if h.Timeout != nil {
		return h.Timeout.Value
	}
	return DefaultHookTimeout
}

// CommandList is one entry or a sequence of them; the wrappers below exist so
// a parse error can name its key.
type CommandList []HookCommand

// SetupCommands is the file-level setup key.
type SetupCommands CommandList

func (s *SetupCommands) UnmarshalYAML(value any) error {
	cmds, err := unmarshalCommandList(value, "setup")
	if err != nil {
		return err
	}
	*s = SetupCommands(cmds)
	return nil
}

// TeardownCommands is the file-level teardown key.
type TeardownCommands CommandList

func (td *TeardownCommands) UnmarshalYAML(value any) error {
	cmds, err := unmarshalCommandList(value, "teardown")
	if err != nil {
		return err
	}
	*td = TeardownCommands(cmds)
	return nil
}

// unmarshalCommandList decodes the two accepted CommandList shapes, naming
// key (setup or teardown) in every error. Only a sequence's items may take
// the mapping form -- a bare (non-list) value must be a command string, so a
// lone mapping like `setup: {cmd: ...}` falls through to the final error. An
// explicit `setup: null`/`teardown: null` is treated the same as an absent
// key, matching Matrix and SnapshotCheck.
func unmarshalCommandList(value any, key string) (CommandList, error) {
	if value == nil {
		return nil, nil
	}
	if seq, ok := value.([]any); ok {
		if len(seq) == 0 {
			return nil, fmt.Errorf("%s: must list at least one command", key)
		}
		cmds := make(CommandList, 0, len(seq))
		for i, item := range seq {
			hc, err := hookCommandFromValue(item, key, fmt.Sprintf("command %d", i+1))
			if err != nil {
				return nil, err
			}
			cmds = append(cmds, hc)
		}
		return cmds, nil
	}
	if _, isMapping := value.(*yamlfixed.Map); !isMapping {
		hc, err := hookCommandFromValue(value, key, "command")
		if err != nil {
			return nil, err
		}
		return CommandList{hc}, nil
	}
	return nil, fmt.Errorf("%s must be a command string or a list of command strings", key)
}

// hookCommandFromValue decodes one setup/teardown entry into a HookCommand:
// either a bare command string (commandFromValue's rules) or a mapping with
// cmd plus optional env, stdin_file, and timeout. It is NOT an Unmarshaler
// method -- the automatic dispatch has no room to carry key
// ("setup"/"teardown") and label ("command"/"command N") through to every
// error, so unmarshalCommandList calls it directly per item. A parsed
// *yamlfixed.Map can never hold a duplicate key (the parser rejects that
// before this ever runs), so no manual duplicate-key bookkeeping is needed.
func hookCommandFromValue(value any, key, label string) (HookCommand, error) {
	if _, isSequence := value.([]any); isSequence {
		return HookCommand{}, fmt.Errorf("%s: %s must be a command string or a mapping (cmd, env, stdin_file, timeout)", key, label)
	}
	m, isMapping := value.(*yamlfixed.Map)
	if !isMapping {
		cmd, err := commandFromValue(value, key, label)
		if err != nil {
			return HookCommand{}, err
		}
		return HookCommand{Cmd: cmd}, nil
	}
	var hc HookCommand
	for _, k := range m.Keys {
		v, _ := m.Get(k)
		switch k {
		case "cmd":
			cmd, err := commandFromValue(v, key, label)
			if err != nil {
				return HookCommand{}, err
			}
			hc.Cmd = cmd
		case "env":
			envMap, ok := v.(*yamlfixed.Map)
			if !ok {
				return HookCommand{}, fmt.Errorf("%s: %s: env must be a mapping of variable name to value", key, label)
			}
			env := make(map[string]string, envMap.Len())
			for _, envName := range envMap.Keys {
				envVal, _ := envMap.Get(envName)
				s, ok := envVal.(string)
				if !ok {
					return HookCommand{}, fmt.Errorf("%s: %s: env: %q must be a string", key, label, envName)
				}
				env[envName] = s
			}
			hc.Env = env
		case "stdin_file":
			s, ok := v.(string)
			if !ok || s == "" {
				return HookCommand{}, fmt.Errorf("%s: %s: stdin_file must be a non-empty string", key, label)
			}
			hc.StdinFile = s
		case "timeout":
			var d Duration
			if err := d.UnmarshalYAML(v); err != nil {
				return HookCommand{}, fmt.Errorf("%s: %s: %w", key, label, err)
			}
			if d.Value <= 0 {
				return HookCommand{}, fmt.Errorf("%s: %s: timeout must be greater than 0 (omit it to use the default %s)", key, label, DefaultHookTimeout)
			}
			hc.Timeout = &d
		default:
			return HookCommand{}, fmt.Errorf("%s: %s: unknown key %q (allowed: cmd, env, stdin_file, timeout)", key, label, k)
		}
	}
	if hc.Cmd == "" {
		return HookCommand{}, fmt.Errorf("%s: %s: must set cmd", key, label)
	}
	return hc, nil
}

// commandFromValue validates one command scalar, naming key (setup or
// teardown) and label ("command" or "command N") in every error. Only true
// strings are commands: a quoted-looking bare 42 parses as the int 42, which
// could never be a meaningful command.
func commandFromValue(value any, key, label string) (string, error) {
	s, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s: %s must be a string", key, label)
	}
	if strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("%s: %s must not be empty", key, label)
	}
	if msg := bannedRedirect(s); msg != "" {
		return "", fmt.Errorf("%s: %s: %s", key, label, msg)
	}
	return s, nil
}

// A heredoc embeds a file inline, bypassing dats' fixtures.
const heredocBanMessage = "must not use a shell heredoc (<<WORD) -- write the file and pull it in with inputs.files/inputs.copy or shared.files/shared.copy instead"

// A herestring feeds stdin from the end of the line, against the data flow.
const herestringBanMessage = "must not use a shell herestring (<<<) -- use inputs.stdin (or a pipe within cmd) instead of redirecting from the end of the line"

// bannedRedirect reports why s is rejected, or "". The first "<<" decides
// which message applies.
func bannedRedirect(s string) string {
	idx := strings.Index(s, "<<")
	if idx == -1 {
		return ""
	}
	if idx+2 < len(s) && s[idx+2] == '<' {
		return herestringBanMessage
	}
	return heredocBanMessage
}

// Shared declares fixtures written once per file, before setup, and addressed
// as {shared.X}.
type Shared struct {
	// Files maps a local name to content, expanding {shared.X} only.
	Files map[string]string `yaml:"files,omitempty"`
	// Copy maps a local name to a host file, copied in writable. A relative
	// source resolves against the .dats file, and a name cannot also be Files.
	Copy map[string]string `yaml:"copy,omitempty"`
}

// Test represents a single test case
type Test struct {
	Desc    string   `yaml:"desc,omitempty"`
	Exit    ExitCode `yaml:"exit"`
	Cmd     string   `yaml:"cmd"`
	Timeout Duration `yaml:"timeout,omitempty"`
	// Matrix expands the test into one instance per combination; nil is one.
	Matrix  Matrix      `yaml:"matrix,omitempty"`
	Inputs  InputBlock  `yaml:"inputs,omitempty"`
	Outputs OutputBlock `yaml:"outputs,omitempty"`
}

// InputBlock contains stdin, input files, and environment variables
type InputBlock struct {
	Stdin string            `yaml:"stdin,omitempty"`
	Files map[string]string `yaml:"files,omitempty"`
	// Copy maps a local name to a host file, copied in writable: the file the
	// test needs to modify. Resolved and validated exactly like Shared.Copy.
	Copy map[string]string `yaml:"copy,omitempty"`
	// Env adds variables to the inherited environment; values expand as cmd does.
	Env map[string]string `yaml:"env,omitempty"`
}

// ExitCode can be an int or a string like "EXIT_SUCCESS"
type ExitCode struct {
	Value    int
	Variable string // If non-empty, use this variable name instead of Value
}

func (e *ExitCode) UnmarshalYAML(value any) error {
	switch v := value.(type) {
	case int:
		if v < 0 || v > 255 {
			return fmt.Errorf("exit code %d must be in range 0-255", v)
		}
		e.Value = v
		return nil
	case float64:
		// Even 2.0 is rejected: decoding into int would truncate silently.
		return fmt.Errorf("exit code must be an integer in range 0-255, got float %s", formatFloatForError(v))
	case string:
		// A quoted integer counts as its value; else it must be a known name.
		if intVal, err := strconv.Atoi(v); err == nil {
			if intVal < 0 || intVal > 255 {
				return fmt.Errorf("exit code %d must be in range 0-255", intVal)
			}
			e.Value = intVal
			return nil
		}
		if !exitCodeNames.Contains(v) {
			return fmt.Errorf("exit %q is not a recognized exit code name (use EXIT_SUCCESS, EXIT_FAILURE, or an integer 0-255)", v)
		}
		e.Variable = v
		return nil
	}
	return fmt.Errorf("exit must be an integer (0-255) or EXIT_SUCCESS/EXIT_FAILURE")
}

// Duration is a per-test timeout. It accepts either a bare integer number of
// seconds (e.g. 5) or a Go duration string (e.g. "500ms", "2s", "1m30s"). A
// zero value means no timeout.
type Duration struct {
	Value time.Duration
}

func (d *Duration) UnmarshalYAML(value any) error {
	switch v := value.(type) {
	case int:
		if v < 0 {
			return fmt.Errorf("timeout %d must not be negative", v)
		}
		d.Value = time.Duration(v) * time.Second
		return nil
	case float64:
		// A truncated float turns timeout: 0.9 into no timeout at all.
		// Fractional seconds are a duration string instead.
		return fmt.Errorf("timeout must be an integer number of seconds or a duration string (e.g. \"900ms\", \"1.5s\"), got float %s", formatFloatForError(v))
	case string:
		// A quoted bare integer means seconds, like the unquoted form.
		if intVal, err := strconv.Atoi(v); err == nil {
			if intVal < 0 {
				return fmt.Errorf("timeout %d must not be negative", intVal)
			}
			d.Value = time.Duration(intVal) * time.Second
			return nil
		}
		parsed, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("invalid timeout %q: %w", v, err)
		}
		if parsed < 0 {
			return fmt.Errorf("timeout %q must not be negative", v)
		}
		d.Value = parsed
		return nil
	}
	return fmt.Errorf("timeout must be an integer (seconds) or duration string")
}

// formatFloatForError renders a rejected float. The parser keeps no source
// text, so "1e3" is reported as its resolved value.
func formatFloatForError(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// OutputBlock contains all output validations
type OutputBlock struct {
	Stdout    OutputCheck          `yaml:"stdout,omitempty"`
	Stderr    OutputCheck          `yaml:"stderr,omitempty"`
	NotStdout OutputCheck          `yaml:"!stdout,omitempty"`
	NotStderr OutputCheck          `yaml:"!stderr,omitempty"`
	Files     map[string]FileCheck `yaml:"files,omitempty"`
	NotFiles  map[string]FileCheck `yaml:"!files,omitempty"`
	// Snapshot is a value type, so matrix copyTest duplicates it by copy.
	Snapshot SnapshotCheck `yaml:"snapshot,omitempty"`
	// JSONOutput is wrapped so an explicit null differs from an absent key.
	JSONOutput jsonOutputExpectation `yaml:"json_output,omitempty"`
}

// jsonOutputExpectation wraps outputs.json_output. yaml-fixed calls
// UnmarshalYAML only for a key that is present, so set IS "the key was given",
// explicit null included.
type jsonOutputExpectation struct {
	set   bool
	value any
}

func (j *jsonOutputExpectation) UnmarshalYAML(value any) error {
	j.set = true
	j.value = yamlfixed.ToPlain(value)
	return nil
}

// HasJSONOutput reports whether a json_output expectation was specified.
func (o *OutputBlock) HasJSONOutput() bool {
	return o.JSONOutput.set
}

// JSONOutputValue returns the expectation as plain Go values. The error is
// kept for callers and is always nil: parsing resolved the value already.
func (o *OutputBlock) JSONOutputValue() (any, error) {
	return o.JSONOutput.value, nil
}

// OutputCheck represents either:
// - A list of patterns to match anywhere in output
// - A map of line numbers to patterns (for line-specific assertions)
type OutputCheck struct {
	Patterns   []string       // patterns to match anywhere
	LineChecks map[int]string // line-specific patterns (0-indexed)
}

// UnmarshalYAML decodes both OutputCheck shapes. An explicit null reads as
// an absent key, matching Matrix and SnapshotCheck.
func (o *OutputCheck) UnmarshalYAML(value any) error {
	if value == nil {
		return nil
	}
	if seq, ok := value.([]any); ok {
		patterns := make([]string, len(seq))
		for i, item := range seq {
			s, ok := item.(string)
			if !ok {
				return fmt.Errorf("output check pattern %d must be a string", i)
			}
			patterns[i] = s
		}
		o.Patterns = patterns
		return nil
	}
	// The parser rejects a duplicate KEY, but "0" and "00" are two keys
	// naming one line number, and only this code knows that collides.
	if m, ok := value.(*yamlfixed.Map); ok {
		o.LineChecks = make(map[int]string, m.Len())
		for _, k := range m.Keys {
			lineNum, err := strconv.Atoi(k)
			if err != nil {
				return fmt.Errorf("line check key must be an integer, got %q", k)
			}
			if lineNum < 0 {
				return fmt.Errorf("line number must be >= 0, got %d", lineNum)
			}
			if _, dup := o.LineChecks[lineNum]; dup {
				return fmt.Errorf("duplicate line number %d in output check", lineNum)
			}
			v, _ := m.Get(k)
			pattern, ok := v.(string)
			if !ok {
				return fmt.Errorf("line check pattern for line %d must be a string", lineNum)
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

// SnapshotCheck configures golden-file assertions; the zero value is none.
type SnapshotCheck struct {
	// Enabled marks the key present: false and omitted are both the zero value.
	Enabled bool
	Stdout  bool
	Stderr  bool
}

// UnmarshalYAML decodes both snapshot shapes: a bool (true = stdout, false =
// omitted), or a stream mapping with at least one true. Null reads as absent,
// and the parser has already rejected any duplicate key.
func (s *SnapshotCheck) UnmarshalYAML(value any) error {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case bool:
		if v {
			*s = SnapshotCheck{Enabled: true, Stdout: true}
		} else {
			*s = SnapshotCheck{}
		}
		return nil
	case *yamlfixed.Map:
		check := SnapshotCheck{}
		for _, key := range v.Keys {
			if key != "stdout" && key != "stderr" {
				return fmt.Errorf("snapshot: unknown key %q (allowed: stdout, stderr)", key)
			}
			val, _ := v.Get(key)
			enabled, ok := val.(bool)
			if !ok {
				return fmt.Errorf("snapshot: %s must be a boolean", key)
			}
			if key == "stdout" {
				check.Stdout = enabled
			} else {
				check.Stderr = enabled
			}
		}
		// A mapping enabling nothing looks like an assertion and is not one.
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

// IsEmpty means the check asserts nothing; the runner reads that as existence.
func (f FileCheck) IsEmpty() bool {
	return f.Exists == nil && len(f.Match) == 0 && len(f.NotMatch) == 0
}
