package runner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wow-look-at-my/dats/schema"
)

func TestRunTestTimeoutSkipsOtherAssertions(t *testing.T) {
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
	result := r.RunTest(context.Background(), test, tmp, 0)
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
	result := r.RunTest(context.Background(), test, tmp, 0)
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
	result := r.RunTest(context.Background(), test, tmp, 0)
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
	result := r.RunTest(context.Background(), test, tmp, 0)
	assert.False(t, result.Passed)
	require.NotEmpty(t, result.Failures)
	assert.Contains(t, result.Failures[0], "output file name")
	assert.Contains(t, result.Failures[0], "must be a relative path that stays inside the test directory")
	_, statErr := os.Stat(filepath.Join(tmp, "evil.txt"))
	assert.True(t, os.IsNotExist(statErr), "traversal output file must not be written outside the test directory")
}

func TestRunTestNestedOutputFile(t *testing.T) {
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
	result := r.RunTest(context.Background(), test, tmp, 0)
	assert.True(t, result.Passed, "failures: %v", result.Failures)
}

func TestRunTestEmptyFileCheckIsImplicitExists(t *testing.T) {
	// An empty FileCheck ({} or null) used to assert nothing and pass vacuously.
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")

	t.Run("missing file fails", func(t *testing.T) {
		test := &schema.Test{
			Cmd: "true",
			Outputs: schema.OutputBlock{
				Files: map[string]schema.FileCheck{"never-written.txt": {}},
			},
		}
		result := r.RunTest(context.Background(), test, t.TempDir(), 0)
		assert.False(t, result.Passed)
		require.Len(t, result.Failures, 1, "failures: %v", result.Failures)
		assert.Contains(t, result.Failures[0], "file never-written.txt")
		assert.Contains(t, result.Failures[0], "to exist")
	})

	t.Run("present file passes", func(t *testing.T) {
		test := &schema.Test{
			Cmd: "echo data > {outputs.present.txt}",
			Outputs: schema.OutputBlock{
				Files: map[string]schema.FileCheck{"present.txt": {}},
			},
		}
		result := r.RunTest(context.Background(), test, t.TempDir(), 0)
		assert.True(t, result.Passed, "failures: %v", result.Failures)
	})
}

func TestRunTestEmptyNotFileCheckIsImplicitNotExists(t *testing.T) {
	// Under !files an empty FileCheck inverts to "must NOT exist".
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")

	t.Run("missing file passes", func(t *testing.T) {
		test := &schema.Test{
			Cmd: "true",
			Outputs: schema.OutputBlock{
				NotFiles: map[string]schema.FileCheck{"never-written.txt": {}},
			},
		}
		result := r.RunTest(context.Background(), test, t.TempDir(), 0)
		assert.True(t, result.Passed, "failures: %v", result.Failures)
	})

	t.Run("present file fails", func(t *testing.T) {
		test := &schema.Test{
			Cmd: "echo data > {outputs.present.txt}",
			Outputs: schema.OutputBlock{
				NotFiles: map[string]schema.FileCheck{"present.txt": {}},
			},
		}
		result := r.RunTest(context.Background(), test, t.TempDir(), 0)
		assert.False(t, result.Passed)
		require.Len(t, result.Failures, 1, "failures: %v", result.Failures)
		assert.Contains(t, result.Failures[0], "!file present.txt")
		assert.Contains(t, result.Failures[0], "to NOT exist")
	})
}

func TestRunTestEnvVarVisibleToCommand(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	test := &schema.Test{
		Cmd: "echo \"$MY_VAR\"",
		Inputs: schema.InputBlock{
			Env: map[string]string{"MY_VAR": "hello"},
		},
		Outputs: schema.OutputBlock{
			Stdout: schema.OutputCheck{Patterns: []string{"hello"}},
		},
	}
	result := r.RunTest(context.Background(), test, tmp, 0)
	assert.True(t, result.Passed, "failures: %v", result.Failures)
}

func TestRunTestEnvValueExpandsPlaceholders(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	tmp := t.TempDir()

	test := &schema.Test{
		Cmd: "cat \"$CONFIG_PATH\"",
		Inputs: schema.InputBlock{
			Files: map[string]string{"cfg.json": `{"mode":"test"}`},
			Env:   map[string]string{"CONFIG_PATH": "{inputs.cfg.json}"},
		},
		Outputs: schema.OutputBlock{
			Stdout: schema.OutputCheck{Patterns: []string{`{"mode":"test"}`}},
		},
	}
	result := r.RunTest(context.Background(), test, tmp, 0)
	assert.True(t, result.Passed, "failures: %v", result.Failures)
}

func TestRunTestEnvCombinesWithCoverDir(t *testing.T) {
	var buf bytes.Buffer
	coverDir := t.TempDir()
	r := NewRunner(&buf, false, false, coverDir)
	tmp := t.TempDir()

	test := &schema.Test{
		Cmd: "echo \"$MY_VAR $GOCOVERDIR\"",
		Inputs: schema.InputBlock{
			Env: map[string]string{"MY_VAR": "combo"},
		},
		Outputs: schema.OutputBlock{
			Stdout: schema.OutputCheck{Patterns: []string{"combo " + coverDir}},
		},
	}
	result := r.RunTest(context.Background(), test, tmp, 0)
	assert.True(t, result.Passed, "failures: %v", result.Failures)
}

func TestRunTestFileFailuresSortedByName(t *testing.T) {
	// Failing file checks report in sorted-by-name order, not random map iteration order.
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
	result := r.RunTest(context.Background(), test, tmp, 0)
	assert.False(t, result.Passed)
	require.Len(t, result.Failures, 5)
	for i, name := range []string{"aaa.txt", "bbb.txt", "ccc.txt", "ddd.txt", "eee.txt"} {
		assert.Contains(t, result.Failures[i], "file "+name)
	}
}
