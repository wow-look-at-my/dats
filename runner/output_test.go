package runner

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFormatterPrintHeader(t *testing.T) {
	var buf bytes.Buffer
	f := &Formatter{Writer: &buf}
	f.PrintHeader("test.dats", 3)
	assert.Equal(t, "Running test.dats (3 tests)\n\n", buf.String())
}

func TestFormatterPrintResultPassed(t *testing.T) {
	var buf bytes.Buffer
	f := &Formatter{Writer: &buf}
	r := &TestResult{Name: "test one", Index: 0, Passed: true}
	f.PrintResult(r)
	assert.Equal(t, "ok 1 - test one\n", buf.String())
}

func TestFormatterPrintResultFailed(t *testing.T) {
	var buf bytes.Buffer
	f := &Formatter{Writer: &buf}
	r := &TestResult{Name: "test one", Index: 0, Passed: false, Failures: []string{"expected X"}}
	f.PrintResult(r)
	assert.Contains(t, buf.String(), "not ok 1 - test one")
	assert.Contains(t, buf.String(), "# expected X")
}

func TestFormatterPrintResultVerbose(t *testing.T) {
	var buf bytes.Buffer
	f := &Formatter{Writer: &buf, Verbose: true}
	r := &TestResult{
		Name:     "test",
		Index:    0,
		Passed:   false,
		Command:  "echo hi",
		Duration: 100 * time.Millisecond,
		Stdout:   "stdout output\n",
		Stderr:   "stderr output\n",
		Failures: []string{"fail"},
	}
	f.PrintResult(r)
	assert.Contains(t, buf.String(), "command: echo hi")
	assert.Contains(t, buf.String(), "duration:")
	assert.Contains(t, buf.String(), "stdout output")
	assert.Contains(t, buf.String(), "stderr output")
}

func TestFormatterPrintResultVerbosePassed(t *testing.T) {
	var buf bytes.Buffer
	f := &Formatter{Writer: &buf, Verbose: true}
	r := &TestResult{
		Name:    "test",
		Index:   0,
		Passed:  true,
		Command: "echo hi",
		Stdout:  "output\n",
	}
	f.PrintResult(r)
	// Verbose shows command even on pass but not stdout/stderr on pass
	assert.Contains(t, buf.String(), "command: echo hi")
	assert.NotContains(t, buf.String(), "stdout:")
}

func TestFormatterPrintSummary(t *testing.T) {
	var buf bytes.Buffer
	f := &Formatter{Writer: &buf}
	fr := &FileResult{Passed: 2, Failed: 1}
	f.PrintSummary(fr)
	assert.Contains(t, buf.String(), "2/3 passed")
	assert.Contains(t, buf.String(), "1 failed")
}

func TestFormatterPrintSummaryAllPassed(t *testing.T) {
	var buf bytes.Buffer
	f := &Formatter{Writer: &buf}
	fr := &FileResult{Passed: 3, Failed: 0}
	f.PrintSummary(fr)
	assert.Contains(t, buf.String(), "3/3 passed")
	assert.NotContains(t, buf.String(), "failed")
}

func TestFormatterPrintHookFailure(t *testing.T) {
	var buf bytes.Buffer
	f := &Formatter{Writer: &buf}
	f.PrintHookFailure("setup", &CommandFailure{
		Command: "./gen.sh",
		Detail:  "exit code 3",
		Stdout:  "partial output\n",
		Stderr:  "boom\n",
	})
	assert.Equal(t,
		"# setup command failed: ./gen.sh\n"+
			"#   exit code 3\n"+
			"#   stdout:\n"+
			"#     partial output\n"+
			"#   stderr:\n"+
			"#     boom\n",
		buf.String())
}

func TestFormatterPrintHookFailureWithoutCommand(t *testing.T) {
	// Shared fixture write failures have no command to name.
	var buf bytes.Buffer
	f := &Formatter{Writer: &buf}
	f.PrintHookFailure("setup", &CommandFailure{Detail: "shared fixtures: writing shared file x: disk full"})
	assert.Equal(t, "# setup failed: shared fixtures: writing shared file x: disk full\n", buf.String())
}

func TestFormatterPrintHookCommandVerboseOnly(t *testing.T) {
	var quiet bytes.Buffer
	(&Formatter{Writer: &quiet}).PrintHookCommand("teardown", "echo done")
	assert.Equal(t, "", quiet.String())

	var verbose bytes.Buffer
	(&Formatter{Writer: &verbose, Verbose: true}).PrintHookCommand("teardown", "echo done")
	assert.Equal(t, "# teardown: echo done\n", verbose.String())
}

func TestFormatterPrintSummaryTeardownFailed(t *testing.T) {
	var buf bytes.Buffer
	f := &Formatter{Writer: &buf}
	fr := &FileResult{Passed: 2, TeardownFailures: []CommandFailure{{Command: "x", Detail: "exit code 1"}}}
	f.PrintSummary(fr)
	assert.Equal(t, "\n2/2 passed, teardown failed\n", buf.String())
}

func TestFileResultOk(t *testing.T) {
	assert.True(t, (&FileResult{Passed: 1}).Ok())
	assert.False(t, (&FileResult{Failed: 1}).Ok())
	assert.False(t, (&FileResult{SetupFailure: &CommandFailure{}}).Ok())
	assert.False(t, (&FileResult{TeardownFailures: []CommandFailure{{}}}).Ok())
}

func TestFormatterPrintError(t *testing.T) {
	var buf bytes.Buffer
	f := &Formatter{Writer: &buf}
	f.PrintError("something went %s", "wrong")
	assert.Equal(t, "Error: something went wrong\n", buf.String())
}
