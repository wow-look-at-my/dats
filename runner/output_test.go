package runner

import (
	"bytes"
	"testing"
	"time"

	"github.com/wow-look-at-my/testify/assert"
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

func TestFormatterPrintError(t *testing.T) {
	var buf bytes.Buffer
	f := &Formatter{Writer: &buf}
	f.PrintError("something went %s", "wrong")
	assert.Equal(t, "Error: something went wrong\n", buf.String())
}
