package schema

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestSetupCommands_UnmarshalYAML_Forms(t *testing.T) {
	var single SetupCommands
	require.Nil(t, yaml.Unmarshal([]byte("echo one"), &single))
	assert.Equal(t, SetupCommands{{Cmd: "echo one"}}, single)

	var list SetupCommands
	require.Nil(t, yaml.Unmarshal([]byte("- echo one\n- echo two\n"), &list))
	assert.Equal(t, SetupCommands{{Cmd: "echo one"}, {Cmd: "echo two"}}, list)
}

func TestTeardownCommands_UnmarshalYAML_ErrorsNameKey(t *testing.T) {
	// The wrapper types exist so errors can name their key.
	var td TeardownCommands
	err := yaml.Unmarshal([]byte("[]"), &td)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "teardown: must list at least one command")

	var s SetupCommands
	err = yaml.Unmarshal([]byte("[]"), &s)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "setup: must list at least one command")
}

func TestHookCommand_MappingForm(t *testing.T) {
	var s SetupCommands
	require.Nil(t, yaml.Unmarshal([]byte(`
- cmd: echo hi
  env:
    FOO: bar
    BAZ: qux
  stdin_file: fixtures/in.txt
  timeout: 5
`), &s))
	require.Len(t, s, 1)
	hc := s[0]
	assert.Equal(t, "echo hi", hc.Cmd)
	assert.Equal(t, map[string]string{"FOO": "bar", "BAZ": "qux"}, hc.Env)
	assert.Equal(t, "fixtures/in.txt", hc.StdinFile)
	require.NotNil(t, hc.Timeout)
	assert.Equal(t, 5*time.Second, hc.Timeout.Value)
	assert.Equal(t, 5*time.Second, hc.EffectiveTimeout())
}

func TestHookCommand_MappingForm_CmdOnlyDefaultsTimeout(t *testing.T) {
	var s SetupCommands
	require.Nil(t, yaml.Unmarshal([]byte("- cmd: echo hi\n"), &s))
	require.Len(t, s, 1)
	hc := s[0]
	assert.Equal(t, "echo hi", hc.Cmd)
	assert.Nil(t, hc.Env)
	assert.Equal(t, "", hc.StdinFile)
	assert.Nil(t, hc.Timeout)
	assert.Equal(t, DefaultHookTimeout, hc.EffectiveTimeout())
}

func TestHookCommand_BareStringDefaultsTimeout(t *testing.T) {
	var s SetupCommands
	require.Nil(t, yaml.Unmarshal([]byte("echo hi"), &s))
	assert.Equal(t, DefaultHookTimeout, s[0].EffectiveTimeout())
}

func TestHookCommand_MappingForm_MissingCmd(t *testing.T) {
	var s SetupCommands
	err := yaml.Unmarshal([]byte("- env:\n    FOO: bar\n"), &s)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "setup: command 1: must set cmd")
}

func TestHookCommand_MappingForm_UnknownKey(t *testing.T) {
	var s SetupCommands
	err := yaml.Unmarshal([]byte("- cmd: echo hi\n  bogus: 1\n"), &s)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), `unknown key "bogus"`)
}

func TestHookCommand_MappingForm_DuplicateKey(t *testing.T) {
	var s SetupCommands
	err := yaml.Unmarshal([]byte("- cmd: echo hi\n  cmd: echo bye\n"), &s)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "cmd declared more than once")
}

func TestHookCommand_MappingForm_EnvMustBeMapping(t *testing.T) {
	var s SetupCommands
	err := yaml.Unmarshal([]byte("- cmd: echo hi\n  env: not a mapping\n"), &s)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "env must be a mapping of variable name to value")
}

func TestHookCommand_MappingForm_StdinFileMustBeNonEmptyString(t *testing.T) {
	var s SetupCommands
	err := yaml.Unmarshal([]byte("- cmd: echo hi\n  stdin_file: \"\"\n"), &s)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "stdin_file must be a non-empty string")
}

func TestHookCommand_MappingForm_TimeoutMustBePositive(t *testing.T) {
	var s SetupCommands
	err := yaml.Unmarshal([]byte("- cmd: echo hi\n  timeout: 0\n"), &s)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "timeout must be greater than 0")
}

func TestHookCommand_MappingForm_TimeoutRejectsNegative(t *testing.T) {
	var s SetupCommands
	err := yaml.Unmarshal([]byte("- cmd: echo hi\n  timeout: -1\n"), &s)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "must not be negative")
}

func TestExitCode_UnmarshalYAML_Int(t *testing.T) {
	var e ExitCode
	err := yaml.Unmarshal([]byte("42"), &e)
	require.Nil(t, err)

	assert.Equal(t, 42, e.Value)

	assert.Equal(t, "", e.Variable)

}

func TestExitCode_UnmarshalYAML_String(t *testing.T) {
	var e ExitCode
	err := yaml.Unmarshal([]byte("EXIT_SUCCESS"), &e)
	require.Nil(t, err)

	assert.Equal(t, "EXIT_SUCCESS", e.Variable)

	var f ExitCode
	err = yaml.Unmarshal([]byte("EXIT_FAILURE"), &f)
	require.Nil(t, err)

	assert.Equal(t, "EXIT_FAILURE", f.Variable)
}

func TestExitCode_UnmarshalYAML_InvalidString(t *testing.T) {
	invalidCodes := []string{
		"0dfsdfs",
		"abc",
		"EXIT",
		"exit_success",
		"123abc",
		// Well-formed EXIT_* names the runner cannot resolve are rejected at
		// parse time: they could never pass a run.
		"EXIT_BOGUS",
		"EXIT_USAGE",
	}
	for _, code := range invalidCodes {
		var e ExitCode
		err := yaml.Unmarshal([]byte(code), &e)
		assert.NotNil(t, err)

	}
}

func TestExitCode_UnmarshalYAML_OutOfRange(t *testing.T) {
	for _, code := range []string{"-1", "256", "1000"} {
		var e ExitCode
		err := yaml.Unmarshal([]byte(code), &e)
		assert.NotNil(t, err)
	}
}

func TestDuration_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  time.Duration
	}{
		{"bare seconds", "5", 5 * time.Second},
		{"zero", "0", 0},
		{"millis string", "500ms", 500 * time.Millisecond},
		{"compound string", "1m30s", 90 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d Duration
			err := yaml.Unmarshal([]byte(tt.input), &d)
			require.Nil(t, err)
			assert.Equal(t, tt.want, d.Value)
		})
	}
}

func TestDuration_UnmarshalYAML_Invalid(t *testing.T) {
	for _, input := range []string{"-1", "5x", "abc", "-2s"} {
		var d Duration
		err := yaml.Unmarshal([]byte(input), &d)
		assert.NotNil(t, err)
	}
}

func TestOutputCheck_UnmarshalYAML_Patterns(t *testing.T) {
	var o OutputCheck
	err := yaml.Unmarshal([]byte(`["pattern1", "pattern2"]`), &o)
	require.Nil(t, err)

	require.Equal(t, 2, len(o.Patterns))

	assert.False(t, o.Patterns[0] != "pattern1" || o.Patterns[1] != "pattern2")

}

func TestOutputCheck_UnmarshalYAML_LineChecks(t *testing.T) {
	var o OutputCheck
	err := yaml.Unmarshal([]byte("0: \"^line0$\"\n2: \"^line2$\""), &o)
	require.Nil(t, err)

	require.Equal(t, 2, len(o.LineChecks))

	assert.Equal(t, "^line0$", o.LineChecks[0])

	assert.Equal(t, "^line2$", o.LineChecks[2])

}

func TestOutputCheck_IsEmpty(t *testing.T) {
	tests := []struct {
		name  string
		check OutputCheck
		want  bool
	}{
		{"empty", OutputCheck{}, true},
		{"with patterns", OutputCheck{Patterns: []string{"a"}}, false},
		{"with line checks", OutputCheck{LineChecks: map[int]string{0: "a"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.check.IsEmpty()
			assert.Equal(t, tt.want, got)

		})
	}
}

func TestOutputBlock_UnmarshalYAML(t *testing.T) {
	input := `
stdout:
  - "hello"
stderr:
  - "error"
"!stdout":
  - "bad"
"!stderr":
  - "warning"
files:
  binary:
    exists: true
    match:
      - "ELF"
    notMatch:
      - "corrupted"
"!files":
  error.log:
    exists: false
`
	var o OutputBlock
	err := yaml.Unmarshal([]byte(input), &o)
	require.Nil(t, err)

	assert.False(t, len(o.Stdout.Patterns) != 1 || o.Stdout.Patterns[0] != "hello")

	assert.False(t, len(o.Stderr.Patterns) != 1 || o.Stderr.Patterns[0] != "error")

	assert.False(t, len(o.NotStdout.Patterns) != 1 || o.NotStdout.Patterns[0] != "bad")

	assert.False(t, len(o.NotStderr.Patterns) != 1 || o.NotStderr.Patterns[0] != "warning")

	_, ok := o.Files["binary"]
	assert.True(t, ok)

	assert.False(t, o.Files["binary"].Exists == nil || *o.Files["binary"].Exists != true)

	assert.False(t, len(o.Files["binary"].Match) != 1 || o.Files["binary"].Match[0] != "ELF")

	assert.False(t, len(o.Files["binary"].NotMatch) != 1 || o.Files["binary"].NotMatch[0] != "corrupted")

	_, ok = o.NotFiles["error.log"]
	assert.True(t, ok)

	assert.False(t, o.NotFiles["error.log"].Exists == nil || *o.NotFiles["error.log"].Exists != false)

}

func TestOutputBlock_JSONOutput(t *testing.T) {
	// Present: an object value
	var o OutputBlock
	err := yaml.Unmarshal([]byte("json_output:\n  name: dats\n  count: 2\n"), &o)
	require.Nil(t, err)
	require.True(t, o.HasJSONOutput())
	v, err := o.JSONOutputValue()
	require.Nil(t, err)
	m, ok := v.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "dats", m["name"])
	assert.Equal(t, 2, m["count"])

	// Present: an explicit null expectation is still "specified"
	var oNull OutputBlock
	err = yaml.Unmarshal([]byte("json_output: null\n"), &oNull)
	require.Nil(t, err)
	require.True(t, oNull.HasJSONOutput())
	v, err = oNull.JSONOutputValue()
	require.Nil(t, err)
	assert.Nil(t, v)

	// Absent: no json_output key
	var oAbsent OutputBlock
	err = yaml.Unmarshal([]byte("stdout:\n  - hi\n"), &oAbsent)
	require.Nil(t, err)
	assert.False(t, oAbsent.HasJSONOutput())
}

func TestSnapshotCheck_UnmarshalYAML_Forms(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  SnapshotCheck
	}{
		{"scalar true snapshots stdout", "true", SnapshotCheck{Enabled: true, Stdout: true}},
		{"scalar false is the zero value", "false", SnapshotCheck{}},
		{"stderr only", "stderr: true", SnapshotCheck{Enabled: true, Stderr: true}},
		{"stdout only", "stdout: true", SnapshotCheck{Enabled: true, Stdout: true}},
		{"both streams", "stdout: true\nstderr: true", SnapshotCheck{Enabled: true, Stdout: true, Stderr: true}},
		{"explicit false stream", "stdout: true\nstderr: false", SnapshotCheck{Enabled: true, Stdout: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s SnapshotCheck
			require.Nil(t, yaml.Unmarshal([]byte(tt.input), &s))
			assert.Equal(t, tt.want, s)
		})
	}
}

func TestSnapshotCheck_UnmarshalYAML_Errors(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"non-bool scalar", "banana", "snapshot: must be true, false, or a mapping of stream booleans (stdout, stderr)"},
		{"sequence", "[true]", "snapshot: must be true, false, or a mapping of stream booleans (stdout, stderr)"},
		// Iterating the mapping node directly bypasses yaml.v3's own
		// duplicate-key detection, so the unmarshaler must catch this itself.
		{"duplicate key", "stdout: true\nstdout: false", "snapshot: stdout declared more than once"},
		{"unknown key", "files: true", `snapshot: unknown key "files" (allowed: stdout, stderr)`},
		{"non-bool value", "stdout: 1", "snapshot: stdout must be a boolean"},
		{"sequence value", "stderr: [true]", "snapshot: stderr must be a boolean"},
		{"empty map", "{}", "snapshot: must enable at least one of stdout, stderr"},
		{"single false stream", "stdout: false", "snapshot: must enable at least one of stdout, stderr"},
		{"all-false map", "stdout: false\nstderr: false", "snapshot: must enable at least one of stdout, stderr"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s SnapshotCheck
			err := yaml.Unmarshal([]byte(tt.input), &s)
			require.NotNil(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestOutputBlock_SnapshotAbsentAndNull(t *testing.T) {
	// An omitted snapshot key stays the zero value...
	var absent OutputBlock
	require.Nil(t, yaml.Unmarshal([]byte("stdout:\n  - hi\n"), &absent))
	assert.Equal(t, SnapshotCheck{}, absent.Snapshot)

	// ...and so does an explicit null (yaml.v3 never invokes the unmarshaler
	// for null nodes), matching how `matrix:` with explicit null is absent.
	var null OutputBlock
	require.Nil(t, yaml.Unmarshal([]byte("snapshot: null\n"), &null))
	assert.Equal(t, SnapshotCheck{}, null.Snapshot)
}

func TestTestFile_UnmarshalYAML(t *testing.T) {
	input := `
tests:
  - desc: test one
    exit: 0
    cmd: echo hello
    outputs:
      stdout:
        - "hello"
  - desc: test two
    exit: EXIT_FAILURE
    cmd: exit 1
`
	var tf TestFile
	err := yaml.Unmarshal([]byte(input), &tf)
	require.Nil(t, err)

	require.Equal(t, 2, len(tf.Tests))

	assert.Equal(t, "test one", tf.Tests[0].Desc)

	assert.Equal(t, 0, tf.Tests[0].Exit.Value)

	assert.Equal(t, "EXIT_FAILURE", tf.Tests[1].Exit.Variable)

}
