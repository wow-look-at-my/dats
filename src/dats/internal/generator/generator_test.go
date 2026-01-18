package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mhaynie/bats-declarative/src/dats/internal/schema"
)

func TestBashEscape(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "$'hello'"},
		{"it's", "$'it\\'s'"},
		{"line1\nline2", "$'line1\\nline2'"},
		{"tab\there", "$'tab\\there'"},
		{`back\slash`, "$'back\\\\slash'"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := bashEscape(tt.input); got != tt.want {
				t.Errorf("bashEscape(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBashEscapeDouble(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", `"hello"`},
		{`say "hi"`, `"say \"hi\""`},
		{"$var", `"\$var"`},
		{"`cmd`", "\"\\`cmd\\`\""},
		{`back\slash`, `"back\\slash"`},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := bashEscapeDouble(tt.input); got != tt.want {
				t.Errorf("bashEscapeDouble(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExpandPlaceholders(t *testing.T) {
	inputPaths := map[string]string{
		"input.txt": "fixtures/test/0/inputs/input.txt",
	}
	outputPaths := map[string]string{
		"output.bin": "fixtures/test/0/outputs/output.bin",
	}

	tests := []struct {
		name string
		cmd  string
		want string
	}{
		{
			"no placeholders",
			"echo hello",
			"echo hello",
		},
		{
			"input placeholder",
			"cat {inputs.input.txt}",
			`cat "$BATS_TEST_DIRNAME/fixtures/test/0/inputs/input.txt"`,
		},
		{
			"output placeholder",
			"cp file {outputs.output.bin}",
			`cp file "$BATS_TEST_DIRNAME/fixtures/test/0/outputs/output.bin"`,
		},
		{
			"both placeholders",
			"process {inputs.input.txt} -o {outputs.output.bin}",
			`process "$BATS_TEST_DIRNAME/fixtures/test/0/inputs/input.txt" -o "$BATS_TEST_DIRNAME/fixtures/test/0/outputs/output.bin"`,
		},
		{
			"unknown placeholder preserved",
			"cat {inputs.unknown}",
			"cat {inputs.unknown}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expandPlaceholders(tt.cmd, inputPaths, outputPaths, "/base")
			if got != tt.want {
				t.Errorf("expandPlaceholders() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGenerateOutputAssertions(t *testing.T) {
	tests := []struct {
		name     string
		check    schema.OutputCheck
		negative bool
		want     []string
	}{
		{
			"patterns positive",
			schema.OutputCheck{Patterns: []string{"hello", "world"}},
			false,
			[]string{"assert_output --partial $'hello'", "assert_output --partial $'world'"},
		},
		{
			"patterns negative",
			schema.OutputCheck{Patterns: []string{"error"}},
			true,
			[]string{"refute_output --partial $'error'"},
		},
		{
			"line checks positive",
			schema.OutputCheck{LineChecks: map[int]string{0: "^first$", 2: "^third$"}},
			false,
			[]string{"assert_line --index 0 --regexp $'^first$'", "assert_line --index 2 --regexp $'^third$'"},
		},
		{
			"line checks negative",
			schema.OutputCheck{LineChecks: map[int]string{1: "bad"}},
			true,
			[]string{"refute_line --index 1 --regexp $'bad'"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf strings.Builder
			generateOutputAssertions(&buf, "output", tt.check, tt.negative)
			got := buf.String()
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q, got:\n%s", want, got)
				}
			}
		})
	}
}

func TestGenerator_Generate(t *testing.T) {
	// Create temp directories
	tmpDir := t.TempDir()
	inputDir := filepath.Join(tmpDir, "input")
	outputDir := filepath.Join(tmpDir, "output")
	runtimeDir := filepath.Join(tmpDir, "runtime")

	os.MkdirAll(inputDir, 0755)
	os.MkdirAll(outputDir, 0755)
	os.MkdirAll(runtimeDir, 0755)

	// Create a simple .dats file (no desc - should use cmd as test name)
	datsContent := `
tests:
  - exit: 0
    cmd: echo hello
    outputs:
      stdout:
        - "hello"
`
	datsPath := filepath.Join(inputDir, "test.dats")
	os.WriteFile(datsPath, []byte(datsContent), 0644)

	gen := &Generator{
		InputPath:  datsPath,
		OutputDir:  outputDir,
		RuntimeDir: runtimeDir,
	}

	result, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	// Check bats file was created
	if _, err := os.Stat(result.BatsFile); os.IsNotExist(err) {
		t.Errorf("BatsFile not created: %s", result.BatsFile)
	}

	// Check content - test name should be auto-generated from cmd
	content, _ := os.ReadFile(result.BatsFile)
	contentStr := string(content)

	if !strings.Contains(contentStr, "@test \"echo hello\"") {
		t.Errorf("missing test declaration in output:\n%s", contentStr)
	}
	if !strings.Contains(contentStr, "run echo hello") {
		t.Errorf("missing run command in output:\n%s", contentStr)
	}
	if !strings.Contains(contentStr, "assert_exit_code 0") {
		t.Errorf("missing exit code assertion in output:\n%s", contentStr)
	}
}

func TestGenerator_Generate_WithInputs(t *testing.T) {
	tmpDir := t.TempDir()
	inputDir := filepath.Join(tmpDir, "input")
	outputDir := filepath.Join(tmpDir, "output")
	runtimeDir := filepath.Join(tmpDir, "runtime")

	os.MkdirAll(inputDir, 0755)
	os.MkdirAll(outputDir, 0755)
	os.MkdirAll(runtimeDir, 0755)

	datsContent := `
tests:
  - desc: cat test
    exit: 0
    inputs:
      files:
        data.txt: |
          hello world
    cmd: cat {inputs.data.txt}
    outputs:
      stdout:
        - "hello world"
`
	datsPath := filepath.Join(inputDir, "test.dats")
	os.WriteFile(datsPath, []byte(datsContent), 0644)

	gen := &Generator{
		InputPath:  datsPath,
		OutputDir:  outputDir,
		RuntimeDir: runtimeDir,
	}

	result, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	// Check fixture file was created
	if len(result.InputFiles) != 1 {
		t.Errorf("expected 1 input file, got %d", len(result.InputFiles))
	}

	// Verify fixture file exists on disk
	for path := range result.InputFiles {
		fullPath := filepath.Join(outputDir, path)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			t.Errorf("fixture file not created: %s", fullPath)
		}
	}
}
