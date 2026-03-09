package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
)

func TestRunTests(t *testing.T) {
	tmp := t.TempDir()
	datsFile := filepath.Join(tmp, "test.dats")
	content := `tests:
  - desc: simple test
    cmd: echo hello
    outputs:
      stdout:
        - "hello"
`
	require.Nil(t, os.WriteFile(datsFile, []byte(content), 0644))

	err := runTests([]string{datsFile})
	assert.Nil(t, err)
}

func TestRunTestsInvalidFile(t *testing.T) {
	err := runTests([]string{"/nonexistent/test.dats"})
	assert.NotNil(t, err)
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

	err := runTests([]string{f1, f2})
	assert.Nil(t, err)
}
