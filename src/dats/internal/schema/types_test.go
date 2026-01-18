package schema

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestExitCode_UnmarshalYAML_Int(t *testing.T) {
	var e ExitCode
	err := yaml.Unmarshal([]byte("42"), &e)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Value != 42 {
		t.Errorf("expected Value=42, got %d", e.Value)
	}
	if e.Variable != "" {
		t.Errorf("expected Variable='', got %q", e.Variable)
	}
}

func TestExitCode_UnmarshalYAML_String(t *testing.T) {
	var e ExitCode
	err := yaml.Unmarshal([]byte("EXIT_SUCCESS"), &e)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Variable != "EXIT_SUCCESS" {
		t.Errorf("expected Variable='EXIT_SUCCESS', got %q", e.Variable)
	}
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
		if err == nil {
			t.Errorf("expected error for %q, got none", code)
		}
	}
}

func TestExitCode_String(t *testing.T) {
	tests := []struct {
		name     string
		exitCode ExitCode
		want     string
	}{
		{"int zero", ExitCode{Value: 0}, "0"},
		{"int nonzero", ExitCode{Value: 127}, "127"},
		{"variable", ExitCode{Variable: "EXIT_FAILURE"}, "$EXIT_FAILURE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.exitCode.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOutputCheck_UnmarshalYAML_Patterns(t *testing.T) {
	var o OutputCheck
	err := yaml.Unmarshal([]byte(`["pattern1", "pattern2"]`), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(o.Patterns) != 2 {
		t.Fatalf("expected 2 patterns, got %d", len(o.Patterns))
	}
	if o.Patterns[0] != "pattern1" || o.Patterns[1] != "pattern2" {
		t.Errorf("unexpected patterns: %v", o.Patterns)
	}
}

func TestOutputCheck_UnmarshalYAML_LineChecks(t *testing.T) {
	var o OutputCheck
	err := yaml.Unmarshal([]byte("0: \"^line0$\"\n2: \"^line2$\""), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(o.LineChecks) != 2 {
		t.Fatalf("expected 2 line checks, got %d", len(o.LineChecks))
	}
	if o.LineChecks[0] != "^line0$" {
		t.Errorf("expected line 0 = '^line0$', got %q", o.LineChecks[0])
	}
	if o.LineChecks[2] != "^line2$" {
		t.Errorf("expected line 2 = '^line2$', got %q", o.LineChecks[2])
	}
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
			if got := tt.check.IsEmpty(); got != tt.want {
				t.Errorf("IsEmpty() = %v, want %v", got, tt.want)
			}
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
binary:
  exists: true
  contains:
    - "ELF"
`
	var o OutputBlock
	err := yaml.Unmarshal([]byte(input), &o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(o.Stdout.Patterns) != 1 || o.Stdout.Patterns[0] != "hello" {
		t.Errorf("unexpected stdout: %v", o.Stdout)
	}
	if len(o.Stderr.Patterns) != 1 || o.Stderr.Patterns[0] != "error" {
		t.Errorf("unexpected stderr: %v", o.Stderr)
	}
	if len(o.NotStdout.Patterns) != 1 || o.NotStdout.Patterns[0] != "bad" {
		t.Errorf("unexpected !stdout: %v", o.NotStdout)
	}
	if _, ok := o.Files["binary"]; !ok {
		t.Errorf("expected binary in Files")
	}
	if o.Files["binary"].Exists == nil || *o.Files["binary"].Exists != true {
		t.Errorf("expected binary.exists = true")
	}
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
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(tf.Tests) != 2 {
		t.Fatalf("expected 2 tests, got %d", len(tf.Tests))
	}
	if tf.Tests[0].Desc != "test one" {
		t.Errorf("expected desc 'test one', got %q", tf.Tests[0].Desc)
	}
	if tf.Tests[0].Exit.Value != 0 {
		t.Errorf("expected exit 0, got %d", tf.Tests[0].Exit.Value)
	}
	if tf.Tests[1].Exit.Variable != "EXIT_FAILURE" {
		t.Errorf("expected exit EXIT_FAILURE, got %q", tf.Tests[1].Exit.Variable)
	}
}
