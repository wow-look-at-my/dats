package runner

// Tests for matrix (parameterized) test execution: tests are expanded into
// instances up front, so the header count, instance numbering, per-instance
// fixture isolation, summary counts, and setup-failure reporting all operate
// on the expanded list, and every reported line carries the instance label.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunFileMatrixExpansionOrderAndLabels(t *testing.T) {
	// A 2x3 matrix runs as 6 instances: declaration order, last variable
	// fastest, every line labeled, header and summary counting instances.
	path := writeRunnerDats(t, `
tests:
	- desc: combo
	  cmd: echo "{matrix.a}-{matrix.b}"
	  matrix:
		a:
			- 1
			- 2
		b:
			- x
			- y
			- z
	  outputs:
		stdout:
			- "{matrix.a}-{matrix.b}"
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 6, result.Passed)
	assert.Equal(t, 0, result.Failed)
	require.Len(t, result.Results, 6)
	assert.True(t, result.Ok())

	expected := fmt.Sprintf(`Running %s (6 tests)

ok 1 - combo [a=1, b=x]
ok 2 - combo [a=1, b=y]
ok 3 - combo [a=1, b=z]
ok 4 - combo [a=2, b=x]
ok 5 - combo [a=2, b=y]
ok 6 - combo [a=2, b=z]

6/6 passed
`, path)
	assert.Equal(t, expected, buf.String())
}

func TestRunFileMatrixInstanceIsolation(t *testing.T) {
	// Instances declare the same fixture name with matrix-driven contents;
	// each instance's test directory is its own, so each command sees only
	// its instance's file.
	path := writeRunnerDats(t, `
tests:
	- desc: isolated
	  cmd: cat {inputs.data.txt}
	  matrix:
		v:
			- alpha
			- beta
	  inputs:
		files:
			data.txt: payload {matrix.v}
	  outputs:
		stdout:
			- payload {matrix.v}
		!stdout:
			- payload alpha payload beta
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 2, result.Passed, "output:\n%s", buf.String())
	assert.Equal(t, 0, result.Failed)
}

func TestRunFileMatrixStdinSubstituted(t *testing.T) {
	// inputs.stdin gets matrix substitution (at expansion time) even though
	// it never gets runtime placeholder expansion; the substituted text must
	// reach the process.
	path := writeRunnerDats(t, `
tests:
	- desc: stdin
	  cmd: cat
	  matrix:
		g:
			- hello
			- howdy
	  inputs:
		stdin: greeting={matrix.g}
	  outputs:
		stdout:
			- greeting={matrix.g}
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 2, result.Passed, "output:\n%s", buf.String())
}

func TestRunFileMatrixJSONOutputSubstituted(t *testing.T) {
	path := writeRunnerDats(t, `
tests:
	- desc: json
	  cmd: "printf '{\"greeting\": \"%s\"}' {matrix.g}"
	  matrix:
		g:
			- hello
			- howdy
	  outputs:
		json_output:
			greeting: "{matrix.g}"
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 2, result.Passed, "output:\n%s", buf.String())
}

func TestRunFileMatrixSingleValueStillLabeled(t *testing.T) {
	path := writeRunnerDats(t, `
tests:
	- desc: solo
	  cmd: echo one
	  matrix:
		k:
			- v
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 1, result.Passed)
	assert.Contains(t, buf.String(), "Running "+path+" (1 tests)")
	assert.Contains(t, buf.String(), "ok 1 - solo [k=v]")
}

func TestRunFileMatrixEmptyDescFallsBackToSubstitutedCmd(t *testing.T) {
	path := writeRunnerDats(t, `
tests:
	- cmd: echo {matrix.x}
	  matrix:
		x:
			- a
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 1, result.Passed)
	assert.Contains(t, buf.String(), "ok 1 - echo a [x=a]")
}

func TestRunFileMatrixFailingInstanceLabeled(t *testing.T) {
	// Exactly one combination fails; its "not ok" line must identify the
	// instance by label, and the counts stay instance counts.
	path := writeRunnerDats(t, `
tests:
	- desc: check
	  cmd: test "{matrix.a}{matrix.b}" != "2y"
	  matrix:
		a:
			- 1
			- 2
		b:
			- x
			- y
			- z
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 5, result.Passed)
	assert.Equal(t, 1, result.Failed)
	assert.False(t, result.Ok())

	out := buf.String()
	assert.Contains(t, out, "not ok 5 - check [a=2, b=y]")
	assert.Contains(t, out, "5/6 passed, 1 failed")
	require.Len(t, result.Results, 6)
	assert.Contains(t, result.Results[4].Failures[0], "expected exit code 0, got 1")
}

func TestRunFileMatrixSetupFailureReportsEveryInstance(t *testing.T) {
	// On file setup failure the EXPANDED list is reported: a 2x2 matrix test
	// plus a plain test is 5 failures (never "skipped"), labels included,
	// and teardown still runs.
	marker := filepath.Join(t.TempDir(), "teardown-ran.txt")
	path := writeRunnerDats(t, `
setup:
	- exit 3
teardown: touch `+marker+`
tests:
	- desc: combo
	  cmd: echo "{matrix.a}-{matrix.b}"
	  matrix:
		a: [1, 2]
		b: [p, q]
	- desc: plain
	  cmd: echo plain
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 0, result.Passed)
	assert.Equal(t, 5, result.Failed)
	require.Len(t, result.Results, 5)
	for _, tr := range result.Results {
		require.Len(t, tr.Failures, 1)
		assert.Contains(t, tr.Failures[0], "file setup failed")
	}

	out := buf.String()
	assert.Contains(t, out, "(5 tests)")
	assert.Contains(t, out, "not ok 1 - combo [a=1, b=p]")
	assert.Contains(t, out, "not ok 2 - combo [a=1, b=q]")
	assert.Contains(t, out, "not ok 3 - combo [a=2, b=p]")
	assert.Contains(t, out, "not ok 4 - combo [a=2, b=q]")
	assert.Contains(t, out, "not ok 5 - plain")
	assert.Contains(t, out, "0/5 passed, 5 failed")
	assert.NotContains(t, out, "skip")

	_, statErr := os.Stat(marker)
	assert.Nil(t, statErr, "teardown must run even when setup failed")
}

func TestRunFileMatrixValueWithSharedPlaceholderExpandsAtRuntime(t *testing.T) {
	// Matrix substitution happens first; a matrix value carrying a
	// {shared.X} placeholder then behaves like any other text in the
	// command, expanding at runtime.
	path := writeRunnerDats(t, `
shared:
	files:
		config.json: "{\"debug\": true}"
tests:
	- desc: shared-path
	  cmd: cat {matrix.path}
	  matrix:
		path:
			- "{shared.config.json}"
	  outputs:
		stdout:
			- "\"debug\": true"
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 1, result.Passed, "output:\n%s", buf.String())
}

func TestRunFileMatrixValueWithMatrixPlaceholderStaysLiteral(t *testing.T) {
	// Single-pass substitution: a matrix value containing a literal
	// {matrix.b} is NOT re-expanded, even though b is declared.
	path := writeRunnerDats(t, `
tests:
	- desc: literal
	  cmd: echo '{matrix.a}'
	  matrix:
		a:
			- "{matrix.b}"
		b:
			- real
	  outputs:
		stdout:
			- matrix.b
		!stdout:
			- real
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 1, result.Passed, "output:\n%s", buf.String())
	assert.Contains(t, buf.String(), "ok 1 - literal [a={matrix.b}, b=real]")
}
