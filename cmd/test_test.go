package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeDats(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.Nil(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

func TestRunTests(t *testing.T) {
	datsFile := writeDats(t, "test.dats", `tests:
	- desc: simple test
	  cmd: echo hello
	  outputs:
		stdout:
			- hello
`)

	var out bytes.Buffer
	err := runTests(context.Background(), []string{datsFile}, &out, 0, nil, "")
	assert.Nil(t, err)
	assert.Contains(t, out.String(), "ok 1 - simple test")
}

func TestRunTestsInvalidFile(t *testing.T) {
	var out bytes.Buffer
	err := runTests(context.Background(), []string{"/nonexistent/test.dats"}, &out, 0, nil, "")
	assert.NotNil(t, err)
	assert.NotErrorIs(t, err, errTestsFailed)
}

func TestRunTestsFailure(t *testing.T) {
	datsFile := writeDats(t, "fail.dats", `tests:
	- desc: failing test
	  cmd: echo wrong
	  outputs:
		stdout:
			- expected-text
`)

	var out bytes.Buffer
	err := runTests(context.Background(), []string{datsFile}, &out, 0, nil, "")
	assert.ErrorIs(t, err, errTestsFailed)
	assert.Contains(t, out.String(), "not ok 1 - failing test")
}

func TestRunTestsTeardownFailure(t *testing.T) {
	// A teardown failure fails the run even when every test passed.
	datsFile := writeDats(t, "teardown.dats", `teardown: exit 1
tests:
	- cmd: echo hi
`)

	var out bytes.Buffer
	err := runTests(context.Background(), []string{datsFile}, &out, 0, nil, "")
	assert.ErrorIs(t, err, errTestsFailed)
	assert.Contains(t, out.String(), "teardown failed")
}

func TestRunTestsMultipleFiles(t *testing.T) {
	tmp := t.TempDir()
	f1 := filepath.Join(tmp, "a.dats")
	f2 := filepath.Join(tmp, "b.dats")
	content := `tests:
	- cmd: echo hi
`
	require.Nil(t, os.WriteFile(f1, []byte(content), 0644))
	require.Nil(t, os.WriteFile(f2, []byte(content), 0644))

	var out bytes.Buffer
	err := runTests(context.Background(), []string{f1, f2}, &out, 0, nil, "")
	assert.Nil(t, err)
	// The multi-file total goes through the runner's writer, not straight to the process stdout.
	assert.Contains(t, out.String(), "Total: 2/2 passed")
}

func TestRunTestsMultipleFilesFailure(t *testing.T) {
	tmp := t.TempDir()
	pass := filepath.Join(tmp, "pass.dats")
	fail := filepath.Join(tmp, "fail.dats")
	require.Nil(t, os.WriteFile(pass, []byte("tests:\n\t- cmd: echo hi\n"), 0644))
	require.Nil(t, os.WriteFile(fail, []byte("tests:\n\t- cmd: exit 3\n"), 0644))

	var out bytes.Buffer
	err := runTests(context.Background(), []string{pass, fail}, &out, 0, nil, "")
	assert.ErrorIs(t, err, errTestsFailed)
	assert.Contains(t, out.String(), "Total: 1/2 passed, 1 failed")
}
