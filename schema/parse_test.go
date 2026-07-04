package schema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTempDats(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.dats")
	require.Nil(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

func TestParseFile_Valid(t *testing.T) {
	path := writeTempDats(t, `
tests:
  - desc: hello
    cmd: echo hi
    outputs:
      stdout:
        - "hi"
`)
	tf, err := ParseFile(path)
	require.Nil(t, err)
	require.Equal(t, 1, len(tf.Tests))
	assert.Equal(t, "echo hi", tf.Tests[0].Cmd)
}

func TestParseFile_InvalidYAML(t *testing.T) {
	path := writeTempDats(t, "tests: [")
	_, err := ParseFile(path)
	assert.NotNil(t, err)
}

func TestParseFile_MissingCmd(t *testing.T) {
	path := writeTempDats(t, `
tests:
  - desc: no command
    exit: 0
`)
	_, err := ParseFile(path)
	assert.NotNil(t, err)
}

func TestParseFile_EmptyTests(t *testing.T) {
	path := writeTempDats(t, "tests: []\n")
	_, err := ParseFile(path)
	assert.NotNil(t, err)
}

func TestParseFile_MissingFile(t *testing.T) {
	_, err := ParseFile("/nonexistent/path/to/file.dats")
	assert.NotNil(t, err)
}
