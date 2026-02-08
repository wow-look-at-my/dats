package schema

import (
	"encoding/xml"
	"testing"
)

func TestExitCode_UnmarshalXMLAttr_Int(t *testing.T) {
	type wrapper struct {
		Exit ExitCode `xml:"exit,attr"`
	}
	var w wrapper
	err := xml.Unmarshal([]byte(`<wrapper exit="42"/>`), &w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Exit.Value != 42 {
		t.Errorf("expected Value=42, got %d", w.Exit.Value)
	}
	if w.Exit.Variable != "" {
		t.Errorf("expected Variable='', got %q", w.Exit.Variable)
	}
}

func TestExitCode_UnmarshalXMLAttr_String(t *testing.T) {
	type wrapper struct {
		Exit ExitCode `xml:"exit,attr"`
	}
	var w wrapper
	err := xml.Unmarshal([]byte(`<wrapper exit="EXIT_SUCCESS"/>`), &w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Exit.Variable != "EXIT_SUCCESS" {
		t.Errorf("expected Variable='EXIT_SUCCESS', got %q", w.Exit.Variable)
	}
}

func TestExitCode_UnmarshalXMLAttr_InvalidString(t *testing.T) {
	type wrapper struct {
		Exit ExitCode `xml:"exit,attr"`
	}
	invalidCodes := []string{
		"0dfsdfs",
		"abc",
		"EXIT",
		"exit_success",
		"123abc",
	}
	for _, code := range invalidCodes {
		var w wrapper
		err := xml.Unmarshal([]byte(`<wrapper exit="`+code+`"/>`), &w)
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

func TestStreamCheck_Match(t *testing.T) {
	input := `<dats><test cmd="echo hi"><stdout><match>hello</match><match>world</match></stdout></test></dats>`
	var tf TestFile
	err := xml.Unmarshal([]byte(input), &tf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tf.Tests[0].Stdout == nil {
		t.Fatal("expected stdout to be non-nil")
	}
	if len(tf.Tests[0].Stdout.Match) != 2 {
		t.Fatalf("expected 2 match patterns, got %d", len(tf.Tests[0].Stdout.Match))
	}
	if tf.Tests[0].Stdout.Match[0] != "hello" || tf.Tests[0].Stdout.Match[1] != "world" {
		t.Errorf("unexpected match patterns: %v", tf.Tests[0].Stdout.Match)
	}
}

func TestStreamCheck_LineChecks(t *testing.T) {
	input := `<dats><test cmd="echo hi"><stdout><line n="0">^line0$</line><line n="2">^line2$</line></stdout></test></dats>`
	var tf TestFile
	err := xml.Unmarshal([]byte(input), &tf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tf.Tests[0].Stdout == nil {
		t.Fatal("expected stdout to be non-nil")
	}
	lines := tf.Tests[0].Stdout.Lines
	if len(lines) != 2 {
		t.Fatalf("expected 2 line checks, got %d", len(lines))
	}
	if lines[0].N != 0 || lines[0].Pattern != "^line0$" {
		t.Errorf("expected line 0 = '^line0$', got n=%d pattern=%q", lines[0].N, lines[0].Pattern)
	}
	if lines[1].N != 2 || lines[1].Pattern != "^line2$" {
		t.Errorf("expected line 2 = '^line2$', got n=%d pattern=%q", lines[1].N, lines[1].Pattern)
	}
}

func TestStreamCheck_NotMatch(t *testing.T) {
	input := `<dats><test cmd="echo hi"><stdout><match>hello</match><not-match>error</not-match><not-match>warning</not-match></stdout></test></dats>`
	var tf TestFile
	err := xml.Unmarshal([]byte(input), &tf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	stdout := tf.Tests[0].Stdout
	if stdout == nil {
		t.Fatal("expected stdout to be non-nil")
	}
	if len(stdout.Match) != 1 || stdout.Match[0] != "hello" {
		t.Errorf("unexpected match: %v", stdout.Match)
	}
	if len(stdout.NotMatch) != 2 || stdout.NotMatch[0] != "error" || stdout.NotMatch[1] != "warning" {
		t.Errorf("unexpected not-match: %v", stdout.NotMatch)
	}
}

func TestFileOutput_Exists(t *testing.T) {
	input := `<dats><test cmd="echo hi">
		<output name="result.txt" exists="true"><match>ELF</match><not-match>corrupted</not-match></output>
		<output name="error.log" exists="false"/>
	</test></dats>`
	var tf TestFile
	err := xml.Unmarshal([]byte(input), &tf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outputs := tf.Tests[0].Outputs
	if len(outputs) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(outputs))
	}

	// First output: exists=true with match/not-match
	if outputs[0].Name != "result.txt" {
		t.Errorf("expected name 'result.txt', got %q", outputs[0].Name)
	}
	if !outputs[0].Exists.Set || !outputs[0].Exists.Value {
		t.Errorf("expected exists=true")
	}
	if len(outputs[0].Match) != 1 || outputs[0].Match[0] != "ELF" {
		t.Errorf("expected match=[ELF], got %v", outputs[0].Match)
	}
	if len(outputs[0].NotMatch) != 1 || outputs[0].NotMatch[0] != "corrupted" {
		t.Errorf("expected not-match=[corrupted], got %v", outputs[0].NotMatch)
	}

	// Second output: exists=false
	if outputs[1].Name != "error.log" {
		t.Errorf("expected name 'error.log', got %q", outputs[1].Name)
	}
	if !outputs[1].Exists.Set || outputs[1].Exists.Value {
		t.Errorf("expected exists=false")
	}
}

func TestInputFile(t *testing.T) {
	input := `<dats><test cmd="cat {inputs.input.txt}">
		<input name="input.txt">Hello, world!</input>
	</test></dats>`
	var tf TestFile
	err := xml.Unmarshal([]byte(input), &tf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tf.Tests[0].Inputs) != 1 {
		t.Fatalf("expected 1 input, got %d", len(tf.Tests[0].Inputs))
	}
	if tf.Tests[0].Inputs[0].Name != "input.txt" {
		t.Errorf("expected name 'input.txt', got %q", tf.Tests[0].Inputs[0].Name)
	}
	if tf.Tests[0].Inputs[0].Content != "Hello, world!" {
		t.Errorf("expected content 'Hello, world!', got %q", tf.Tests[0].Inputs[0].Content)
	}
}

func TestTestFile_FullParse(t *testing.T) {
	input := `<dats>
	<test desc="test one" cmd="echo hello" exit="0">
		<stdout>
			<match>hello</match>
		</stdout>
	</test>
	<test desc="test two" cmd="exit 1" exit="EXIT_FAILURE"/>
</dats>`
	var tf TestFile
	err := xml.Unmarshal([]byte(input), &tf)
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

func TestExistsBool_NotSet(t *testing.T) {
	input := `<dats><test cmd="echo hi">
		<output name="result.txt"><match>hello</match></output>
	</test></dats>`
	var tf TestFile
	err := xml.Unmarshal([]byte(input), &tf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tf.Tests[0].Outputs[0].Exists.Set {
		t.Errorf("expected Exists.Set=false when attribute not present")
	}
}

func TestStdin(t *testing.T) {
	input := `<dats><test cmd="cat"><stdin>Hello from stdin</stdin></test></dats>`
	var tf TestFile
	err := xml.Unmarshal([]byte(input), &tf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tf.Tests[0].Stdin != "Hello from stdin" {
		t.Errorf("expected stdin 'Hello from stdin', got %q", tf.Tests[0].Stdin)
	}
}

func TestExistsBool_InvalidValue(t *testing.T) {
	input := `<dats><test cmd="echo hi"><output name="f.txt" exists="maybe"/></test></dats>`
	var tf TestFile
	err := xml.Unmarshal([]byte(input), &tf)
	if err == nil {
		t.Error("expected error for exists='maybe', got none")
	}
}
