package runner

// Regression tests for RunTest behaviors around timeouts, signal deaths,
// fixture path traversal, nested output files, and deterministic failure
// ordering.

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

func TestRunTestTimeoutSkipsOtherAssertions(t *testing.T) {
	// A timed-out test reports ONLY the timeout: assertions against the
	// partial output would bury the real cause under secondary failures.
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	boolTrue := true
	test := &schema.Test{
		Cmd:     "echo started; sleep 2",
		Timeout: schema.Duration{Value: 100 * time.Millisecond},
		Outputs: schema.OutputBlock{
			Stdout: schema.OutputCheck{Patterns: []string{"never printed"}},
			Files: map[string]schema.FileCheck{
				"missing.txt": {Exists: &boolTrue},
			},
		},
	}
	result := r.RunTest(test, tmp, 0)
	assert.False(t, result.Passed)
	require.Len(t, result.Failures, 1, "failures: %v", result.Failures)
	assert.Contains(t, result.Failures[0], "timed out after")
	// Output produced before the kill is still captured for verbose display.
	assert.Contains(t, result.Stdout, "started")
}

func TestRunTestSignalDeathMessage(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	test := &schema.Test{Cmd: "kill -KILL $$"}
	result := r.RunTest(test, tmp, 0)
	assert.False(t, result.Passed)
	require.NotEmpty(t, result.Failures)
	assert.Contains(t, result.Failures[0], "expected exit code 0, got -1 (killed by signal: killed)")
}

func TestRunTestRejectsTraversalInputName(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	test := &schema.Test{
		Cmd: "true",
		Inputs: schema.InputBlock{
			Files: map[string]string{"../../evil.txt": "pwned"},
		},
	}
	result := r.RunTest(test, tmp, 0)
	assert.False(t, result.Passed)
	require.NotEmpty(t, result.Failures)
	assert.Contains(t, result.Failures[0], "fixture setup:")
	assert.Contains(t, result.Failures[0], "must be a relative path that stays inside the test directory")
	// Nothing may be written at the escaped location.
	_, statErr := os.Stat(filepath.Join(tmp, "evil.txt"))
	assert.True(t, os.IsNotExist(statErr), "traversal input file must not be written outside the test directory")
}

func TestRunTestRejectsTraversalOutputName(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	boolTrue := true
	test := &schema.Test{
		Cmd: "echo pwned > {outputs.../../evil.txt}",
		Outputs: schema.OutputBlock{
			Files: map[string]schema.FileCheck{
				"../../evil.txt": {Exists: &boolTrue},
			},
		},
	}
	result := r.RunTest(test, tmp, 0)
	assert.False(t, result.Passed)
	require.NotEmpty(t, result.Failures)
	assert.Contains(t, result.Failures[0], "output file name")
	assert.Contains(t, result.Failures[0], "must be a relative path that stays inside the test directory")
	_, statErr := os.Stat(filepath.Join(tmp, "evil.txt"))
	assert.True(t, os.IsNotExist(statErr), "traversal output file must not be written outside the test directory")
}

func TestRunTestNestedOutputFile(t *testing.T) {
	// A registered output like sub/out.txt must be writable: its parent
	// directory is created during fixture setup.
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	boolTrue := true
	test := &schema.Test{
		Cmd: "echo nested > {outputs.sub/out.txt}",
		Outputs: schema.OutputBlock{
			Files: map[string]schema.FileCheck{
				"sub/out.txt": {Exists: &boolTrue, Match: []string{"nested"}},
			},
		},
	}
	result := r.RunTest(test, tmp, 0)
	assert.True(t, result.Passed, "failures: %v", result.Failures)
}

func TestRunTestFileFailuresSortedByName(t *testing.T) {
	// Failing file checks report in sorted-by-name order, not random map
	// iteration order.
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	boolTrue := true
	test := &schema.Test{
		Cmd: "true",
		Outputs: schema.OutputBlock{
			Files: map[string]schema.FileCheck{
				"eee.txt": {Exists: &boolTrue},
				"aaa.txt": {Exists: &boolTrue},
				"ddd.txt": {Exists: &boolTrue},
				"bbb.txt": {Exists: &boolTrue},
				"ccc.txt": {Exists: &boolTrue},
			},
		},
	}
	result := r.RunTest(test, tmp, 0)
	assert.False(t, result.Passed)
	require.Len(t, result.Failures, 5)
	for i, name := range []string{"aaa.txt", "bbb.txt", "ccc.txt", "ddd.txt", "eee.txt"} {
		assert.Contains(t, result.Failures[i], "file "+name)
	}
}
