package schema

import (
	"testing"

	"gopkg.in/yaml.v3"
	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
)

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

}

func TestExitCode_UnmarshalYAML_InvalidString(t *testing.T) {
	invalidCodes := []string{
		"0dfsdfs",
		"abc",
		"EXIT",
		"exit_success",
		"123abc",
	}
	for _, code := range invalidCodes {
		var e ExitCode
		err := yaml.Unmarshal([]byte(code), &e)
		assert.NotNil(t, err)

	}
}

func TestExitCode_String(t *testing.T) {
	tests := []struct {
		name		string
		exitCode	ExitCode
		want		string
	}{
		{"int zero", ExitCode{Value: 0}, "0"},
		{"int nonzero", ExitCode{Value: 127}, "127"},
		{"variable", ExitCode{Variable: "EXIT_FAILURE"}, "$EXIT_FAILURE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.exitCode.String()
			assert.Equal(t, tt.want, got)

		})
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
		name	string
		check	OutputCheck
		want	bool
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
