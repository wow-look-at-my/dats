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

func TestParseFile_UnknownKeysRejected(t *testing.T) {
	cases := map[string]string{
		"top level": `
tests:
  - cmd: echo hi
bogus: true
`,
		"test level": `
tests:
  - cmd: echo hi
    stdotu:
      - "typo of stdout at the wrong level"
`,
		"outputs level": `
tests:
  - cmd: echo hi
    outputs:
      stdotu:
        - "typo of stdout"
`,
		"inputs level": `
tests:
  - cmd: echo hi
    inputs:
      file:
        a.txt: "typo of files"
`,
		"file check level": `
tests:
  - cmd: echo hi
    outputs:
      files:
        out.txt:
          matches:
            - "typo of match"
`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseFile(writeTempDats(t, content))
			require.NotNil(t, err)
			assert.Contains(t, err.Error(), "not found")
		})
	}
}

func TestParseFile_SchemaKeyAllowed(t *testing.T) {
	path := writeTempDats(t, `
"$schema": https://github.com/wow-look-at-my/dats/schema.json
tests:
  - cmd: echo hi
`)
	tf, err := ParseFile(path)
	require.Nil(t, err)
	assert.Equal(t, 1, len(tf.Tests))
}

func TestParseFile_EmptyFile(t *testing.T) {
	_, err := ParseFile(writeTempDats(t, ""))
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "no tests defined")
}
