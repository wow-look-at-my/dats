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
	if w.Exit.IntValue() != 42 {
		t.Errorf("expected IntValue()=42, got %d", w.Exit.IntValue())
	}
	if w.Exit.VariableName() != "" {
		t.Errorf("expected VariableName()='', got %q", w.Exit.VariableName())
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
	if w.Exit.VariableName() != "EXIT_SUCCESS" {
		t.Errorf("expected VariableName()='EXIT_SUCCESS', got %q", w.Exit.VariableName())
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
		{"int zero", ExitCode("0"), "0"},
		{"int nonzero", ExitCode("127"), "127"},
		{"variable", ExitCode("EXIT_FAILURE"), "$EXIT_FAILURE"},
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
	var tf Dats
	err := xml.Unmarshal([]byte(input), &tf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tf.Test[0].Stdout == nil {
		t.Fatal("expected stdout to be non-nil")
	}
	if len(tf.Test[0].Stdout.Match) != 2 {
		t.Fatalf("expected 2 match patterns, got %d", len(tf.Test[0].Stdout.Match))
	}
	if tf.Test[0].Stdout.Match[0] != "hello" || tf.Test[0].Stdout.Match[1] != "world" {
		t.Errorf("unexpected match patterns: %v", tf.Test[0].Stdout.Match)
	}
}

func TestStreamCheck_LineChecks(t *testing.T) {
	input := `<dats><test cmd="echo hi"><stdout><line n="0">^line0$</line><line n="2">^line2$</line></stdout></test></dats>`
	var tf Dats
	err := xml.Unmarshal([]byte(input), &tf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tf.Test[0].Stdout == nil {
		t.Fatal("expected stdout to be non-nil")
	}
	lines := tf.Test[0].Stdout.Line
	if len(lines) != 2 {
		t.Fatalf("expected 2 line checks, got %d", len(lines))
	}
	if lines[0].NAttr != 0 || lines[0].Value != "^line0$" {
		t.Errorf("expected line 0 = '^line0$', got n=%d pattern=%q", lines[0].NAttr, lines[0].Value)
	}
	if lines[1].NAttr != 2 || lines[1].Value != "^line2$" {
		t.Errorf("expected line 2 = '^line2$', got n=%d pattern=%q", lines[1].NAttr, lines[1].Value)
	}
}

func TestStreamCheck_NotMatch(t *testing.T) {
	input := `<dats><test cmd="echo hi"><stdout><match>hello</match><not-match>error</not-match><not-match>warning</not-match></stdout></test></dats>`
	var tf Dats
	err := xml.Unmarshal([]byte(input), &tf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	stdout := tf.Test[0].Stdout
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
	var tf Dats
	err := xml.Unmarshal([]byte(input), &tf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outputs := tf.Test[0].Output
	if len(outputs) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(outputs))
	}

	// First output: exists=true with match/not-match
	if outputs[0].NameAttr != "result.txt" {
		t.Errorf("expected name 'result.txt', got %q", outputs[0].NameAttr)
	}
	if outputs[0].ExistsAttr == nil || !*outputs[0].ExistsAttr {
		t.Errorf("expected exists=true")
	}
	if len(outputs[0].Match) != 1 || outputs[0].Match[0] != "ELF" {
		t.Errorf("expected match=[ELF], got %v", outputs[0].Match)
	}
	if len(outputs[0].NotMatch) != 1 || outputs[0].NotMatch[0] != "corrupted" {
		t.Errorf("expected not-match=[corrupted], got %v", outputs[0].NotMatch)
	}

	// Second output: exists=false
	if outputs[1].NameAttr != "error.log" {
		t.Errorf("expected name 'error.log', got %q", outputs[1].NameAttr)
	}
	if outputs[1].ExistsAttr == nil || *outputs[1].ExistsAttr {
		t.Errorf("expected exists=false")
	}
}

func TestInputFile(t *testing.T) {
	input := `<dats><test cmd="cat {inputs.input.txt}">
		<input name="input.txt">Hello, world!</input>
	</test></dats>`
	var tf Dats
	err := xml.Unmarshal([]byte(input), &tf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tf.Test[0].Input) != 1 {
		t.Fatalf("expected 1 input, got %d", len(tf.Test[0].Input))
	}
	if tf.Test[0].Input[0].NameAttr != "input.txt" {
		t.Errorf("expected name 'input.txt', got %q", tf.Test[0].Input[0].NameAttr)
	}
	if tf.Test[0].Input[0].Value != "Hello, world!" {
		t.Errorf("expected content 'Hello, world!', got %q", tf.Test[0].Input[0].Value)
	}
}

func TestDats_FullParse(t *testing.T) {
	input := `<dats>
	<test desc="test one" cmd="echo hello" exit="0">
		<stdout>
			<match>hello</match>
		</stdout>
	</test>
	<test desc="test two" cmd="exit 1" exit="EXIT_FAILURE"/>
</dats>`
	var tf Dats
	err := xml.Unmarshal([]byte(input), &tf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(tf.Test) != 2 {
		t.Fatalf("expected 2 tests, got %d", len(tf.Test))
	}
	if tf.Test[0].DescAttr == nil || *tf.Test[0].DescAttr != "test one" {
		t.Errorf("expected desc 'test one', got %v", tf.Test[0].DescAttr)
	}
	if tf.Test[0].ExitAttr == nil || tf.Test[0].ExitAttr.IntValue() != 0 {
		t.Errorf("expected exit 0")
	}
	if tf.Test[1].ExitAttr == nil || tf.Test[1].ExitAttr.VariableName() != "EXIT_FAILURE" {
		t.Errorf("expected exit EXIT_FAILURE")
	}
}

func TestExistsAttr_NotSet(t *testing.T) {
	input := `<dats><test cmd="echo hi">
		<output name="result.txt"><match>hello</match></output>
	</test></dats>`
	var tf Dats
	err := xml.Unmarshal([]byte(input), &tf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tf.Test[0].Output[0].ExistsAttr != nil {
		t.Errorf("expected ExistsAttr=nil when attribute not present")
	}
}

func TestStdin(t *testing.T) {
	input := `<dats><test cmd="cat"><stdin>Hello from stdin</stdin></test></dats>`
	var tf Dats
	err := xml.Unmarshal([]byte(input), &tf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tf.Test[0].Stdin == nil || *tf.Test[0].Stdin != "Hello from stdin" {
		t.Errorf("expected stdin 'Hello from stdin', got %v", tf.Test[0].Stdin)
	}
}

func TestExistsAttr_InvalidValue(t *testing.T) {
	input := `<dats><test cmd="echo hi"><output name="f.txt" exists="maybe"/></test></dats>`
	var tf Dats
	err := xml.Unmarshal([]byte(input), &tf)
	if err == nil {
		t.Error("expected error for exists='maybe', got none")
	}
}
