package runner

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wow-look-at-my/dats/schema"
)

func TestNewRunner(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(&buf, true, false, "/tmp/cover")
	assert.True(t, r.Verbose)
	assert.False(t, r.KeepTemp)
	assert.Equal(t, "/tmp/cover", r.CoverDir)
	assert.NotNil(t, r.Formatter)
	assert.True(t, r.Formatter.Verbose)
}

func TestRunTestSimplePass(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	test := &schema.Test{
		Cmd:  "echo hello",
		Desc: "simple echo",
	}

	result := r.RunTest(test, tmp, 0)
	assert.True(t, result.Passed)
	assert.Equal(t, "simple echo", result.Name)
	assert.Empty(t, result.Failures)
	assert.Contains(t, result.Stdout, "hello")
}

func TestRunTestUsesCmd(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	test := &schema.Test{Cmd: "echo hi"}
	result := r.RunTest(test, tmp, 0)
	assert.Equal(t, "echo hi", result.Name)
}

func TestRunTestExitCodeFail(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	test := &schema.Test{
		Cmd:  "exit 1",
		Exit: schema.ExitCode{Value: 0},
	}

	result := r.RunTest(test, tmp, 0)
	assert.False(t, result.Passed)
	assert.NotEmpty(t, result.Failures)
}

func TestRunTestStdoutPattern(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	test := &schema.Test{
		Cmd: "echo 'hello world'",
		Outputs: schema.OutputBlock{
			Stdout: schema.OutputCheck{Patterns: []string{"hello"}},
		},
	}

	result := r.RunTest(test, tmp, 0)
	assert.True(t, result.Passed)
}

func TestRunTestStdoutPatternFail(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	test := &schema.Test{
		Cmd: "echo 'hello world'",
		Outputs: schema.OutputBlock{
			Stdout: schema.OutputCheck{Patterns: []string{"missing"}},
		},
	}

	result := r.RunTest(test, tmp, 0)
	assert.False(t, result.Passed)
}

func TestRunTestNotStdout(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	test := &schema.Test{
		Cmd: "echo hello",
		Outputs: schema.OutputBlock{
			NotStdout: schema.OutputCheck{Patterns: []string{"missing"}},
		},
	}
	result := r.RunTest(test, tmp, 0)
	assert.True(t, result.Passed)

	test2 := &schema.Test{
		Cmd: "echo hello",
		Outputs: schema.OutputBlock{
			NotStdout: schema.OutputCheck{Patterns: []string{"hello"}},
		},
	}
	result2 := r.RunTest(test2, tmp, 1)
	assert.False(t, result2.Passed)
}

func TestRunTestStderr(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	test := &schema.Test{
		Cmd: "echo err >&2",
		Outputs: schema.OutputBlock{
			Stderr: schema.OutputCheck{Patterns: []string{"err"}},
		},
	}
	result := r.RunTest(test, tmp, 0)
	assert.True(t, result.Passed)
}

func TestRunTestNotStderr(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	test := &schema.Test{
		Cmd: "echo err >&2",
		Outputs: schema.OutputBlock{
			NotStderr: schema.OutputCheck{Patterns: []string{"missing"}},
		},
	}
	result := r.RunTest(test, tmp, 0)
	assert.True(t, result.Passed)
}

func TestRunTestLineChecks(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	test := &schema.Test{
		Cmd: "printf 'line0\\nline1\\nline2\\n'",
		Outputs: schema.OutputBlock{
			Stdout: schema.OutputCheck{
				LineChecks: map[int]string{0: "^line0$", 2: "^line2$"},
			},
		},
	}
	result := r.RunTest(test, tmp, 0)
	assert.True(t, result.Passed)
}

func TestRunTestStderrLineChecks(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	test := &schema.Test{
		Cmd: "printf 'err0\\nerr1\\n' >&2",
		Outputs: schema.OutputBlock{
			Stderr: schema.OutputCheck{
				LineChecks: map[int]string{0: "^err0$"},
			},
		},
	}
	result := r.RunTest(test, tmp, 0)
	assert.True(t, result.Passed)
}

func TestRunTestWithInputs(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	test := &schema.Test{
		Cmd: "cat {inputs.hello.txt}",
		Inputs: schema.InputBlock{
			Files: map[string]string{"hello.txt": "world"},
		},
		Outputs: schema.OutputBlock{
			Stdout: schema.OutputCheck{Patterns: []string{"world"}},
		},
	}
	result := r.RunTest(test, tmp, 0)
	assert.True(t, result.Passed)
}

func TestRunTestWithStdin(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	test := &schema.Test{
		Cmd: "cat",
		Inputs: schema.InputBlock{
			Stdin: "piped input",
		},
		Outputs: schema.OutputBlock{
			Stdout: schema.OutputCheck{Patterns: []string{"piped input"}},
		},
	}
	result := r.RunTest(test, tmp, 0)
	assert.True(t, result.Passed)
}

func TestRunTestOutputFileExists(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	boolTrue := true
	test := &schema.Test{
		Cmd: "echo content > {outputs.out.txt}",
		Outputs: schema.OutputBlock{
			Files: map[string]schema.FileCheck{
				"out.txt": {Exists: &boolTrue, Match: []string{"content"}},
			},
		},
	}
	result := r.RunTest(test, tmp, 0)
	assert.True(t, result.Passed)
}

func TestRunTestPlaceholderInInputContents(t *testing.T) {
	// An input file's contents can reference {outputs.X}; the command reads
	// the expanded path from the file and writes there, and the files check
	// sees the result. Mirrors script-driven consumers (e.g. interpreters
	// running a fixture program that writes an output file).
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	boolTrue := true
	test := &schema.Test{
		Cmd: "bash {inputs.script.sh}",
		Inputs: schema.InputBlock{
			Files: map[string]string{
				"script.sh": `echo "from script" > "{outputs.out.txt}"`,
			},
		},
		Outputs: schema.OutputBlock{
			Files: map[string]schema.FileCheck{
				"out.txt": {Exists: &boolTrue, Match: []string{"from script"}},
			},
		},
	}
	result := r.RunTest(test, tmp, 0)
	assert.True(t, result.Passed, "failures: %v", result.Failures)
}

func TestRunTestOutputPlaceholderWithoutFilesCheck(t *testing.T) {
	// {outputs.X} resolves to a real path in the outputs directory even when
	// no files check references X, so commands never write stray files named
	// after the literal placeholder.
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	test := &schema.Test{
		Cmd: "echo data > {outputs.free.txt} && cat {outputs.free.txt}",
		Outputs: schema.OutputBlock{
			Stdout: schema.OutputCheck{Patterns: []string{"data"}},
		},
	}
	result := r.RunTest(test, tmp, 0)
	assert.True(t, result.Passed, "failures: %v", result.Failures)

	content, err := os.ReadFile(filepath.Join(tmp, "test-0", "outputs", "free.txt"))
	require.Nil(t, err)
	assert.Contains(t, string(content), "data")
}

func TestRunTestOutputFileNotExists(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	boolFalse := false
	test := &schema.Test{
		Cmd: "echo hi",
		Outputs: schema.OutputBlock{
			Files: map[string]schema.FileCheck{
				"missing.txt": {Exists: &boolFalse},
			},
		},
	}
	result := r.RunTest(test, tmp, 0)
	assert.True(t, result.Passed)
}

func TestRunTestOutputFileNotMatch(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	test := &schema.Test{
		Cmd: "echo content > {outputs.out.txt}",
		Outputs: schema.OutputBlock{
			Files: map[string]schema.FileCheck{
				"out.txt": {NotMatch: []string{"missing"}},
			},
		},
	}
	result := r.RunTest(test, tmp, 0)
	assert.True(t, result.Passed)
}

func TestRunTestNotFiles(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	boolFalse := false
	test := &schema.Test{
		Cmd: "echo hi",
		Outputs: schema.OutputBlock{
			NotFiles: map[string]schema.FileCheck{
				"nonexistent.txt": {Exists: &boolFalse},
			},
		},
	}
	result := r.RunTest(test, tmp, 0)
	assert.True(t, result.Passed)
}

func TestRunTestNotFilesExists(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	// Create the file that !files expects to exist
	outDir := filepath.Join(tmp, "test-0", "outputs")
	require.Nil(t, os.MkdirAll(outDir, 0755))
	require.Nil(t, os.WriteFile(filepath.Join(outDir, "exists.txt"), []byte("data"), 0644))

	boolTrue := true
	test := &schema.Test{
		Cmd: "echo hi",
		Outputs: schema.OutputBlock{
			NotFiles: map[string]schema.FileCheck{
				"exists.txt": {Exists: &boolTrue},
			},
		},
	}
	result := r.RunTest(test, tmp, 0)
	assert.True(t, result.Passed)
}

func TestRunTestNotFilesMatch(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	boolTrue := true
	test := &schema.Test{
		Cmd: "printf data > {outputs.out.txt}",
		Outputs: schema.OutputBlock{
			NotFiles: map[string]schema.FileCheck{
				"out.txt": {
					Exists:   &boolTrue,
					Match:    []string{"data"},
					NotMatch: []string{"error"},
				},
			},
		},
	}
	result := r.RunTest(test, tmp, 0)
	assert.True(t, result.Passed)
}

func TestRunTestNotFilesMatchFails(t *testing.T) {
	// Regression: match under !files used to be silently ignored, so this test
	// would wrongly pass. The file contains "hello" but match expects "goodbye".
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	test := &schema.Test{
		Cmd: "printf hello > {outputs.out.txt}",
		Outputs: schema.OutputBlock{
			NotFiles: map[string]schema.FileCheck{
				"out.txt": {Match: []string{"goodbye"}},
			},
		},
	}
	result := r.RunTest(test, tmp, 0)
	assert.False(t, result.Passed)
}

func TestRunTestTimeout(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	test := &schema.Test{
		Cmd:     "sleep 1",
		Timeout: schema.Duration{Value: 50 * time.Millisecond},
	}
	result := r.RunTest(test, tmp, 0)
	assert.False(t, result.Passed)
	require.True(t, len(result.Failures) > 0)
	assert.Contains(t, result.Failures[0], "timed out after")
}

func TestRunFile(t *testing.T) {
	// Create a temp .dats file
	tmp := t.TempDir()
	datsFile := filepath.Join(tmp, "test.dats")
	content := `tests:
  - desc: echo test
    cmd: echo hello
    outputs:
      stdout:
        - "hello"
  - desc: exit test
    cmd: exit 0
`
	require.Nil(t, os.WriteFile(datsFile, []byte(content), 0644))

	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(datsFile)
	require.Nil(t, err)
	assert.Equal(t, 2, result.Passed)
	assert.Equal(t, 0, result.Failed)
}

func TestRunFileKeepTemp(t *testing.T) {
	tmp := t.TempDir()
	datsFile := filepath.Join(tmp, "test.dats")
	content := `tests:
  - cmd: echo hi
`
	require.Nil(t, os.WriteFile(datsFile, []byte(content), 0644))

	var buf bytes.Buffer
	r := NewRunner(&buf, false, true, "")
	result, err := r.RunFile(datsFile)
	require.Nil(t, err)
	assert.Equal(t, 1, result.Passed)
	assert.Contains(t, buf.String(), "Temp directory:")
}

func TestRunFileInvalidPath(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	_, err := r.RunFile("/nonexistent/path.dats")
	assert.NotNil(t, err)
}

func TestRunFileCoverDir(t *testing.T) {
	tmp := t.TempDir()
	datsFile := filepath.Join(tmp, "test.dats")
	content := `tests:
  - cmd: echo hi
`
	require.Nil(t, os.WriteFile(datsFile, []byte(content), 0644))

	coverDir := filepath.Join(tmp, "coverage")
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, coverDir)
	_, err := r.RunFile(datsFile)
	require.Nil(t, err)

	// Coverage directory should have been created
	_, err = os.Stat(coverDir)
	assert.Nil(t, err)
}

func TestSortedKeys(t *testing.T) {
	m := map[int]string{3: "c", 1: "a", 2: "b"}
	keys := sortedKeys(m)
	assert.Equal(t, []int{1, 2, 3}, keys)
}
