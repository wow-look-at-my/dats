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
	assert.Equal(t, SetupCommands{"echo one"}, single)

	var list SetupCommands
	require.Nil(t, yaml.Unmarshal([]byte("- echo one\n- echo two\n"), &list))
	assert.Equal(t, SetupCommands{"echo one", "echo two"}, list)
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
