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

func TestParseFile_InputEnvAccepted(t *testing.T) {
	// inputs.env parses under KnownFields (unknown keys under inputs are still
	// rejected -- see TestParseFile_UnknownKeysRejected).
	path := writeTempDats(t, `
tests:
  - cmd: echo "$MY_VAR"
    inputs:
      env:
        MY_VAR: hello
        CONFIG_PATH: "{inputs.cfg.json}"
      files:
        cfg.json: "{}"
`)
	tf, err := ParseFile(path)
	require.Nil(t, err)
	require.Equal(t, 1, len(tf.Tests))
	assert.Equal(t, map[string]string{
		"MY_VAR":      "hello",
		"CONFIG_PATH": "{inputs.cfg.json}",
	}, tf.Tests[0].Inputs.Env)
}

func TestParseFile_EmptyFile(t *testing.T) {
	_, err := ParseFile(writeTempDats(t, ""))
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "no tests defined")
}

func TestParseFile_MultiDocumentRejected(t *testing.T) {
	// A second "---" document used to be silently ignored, dropping its tests.
	path := writeTempDats(t, `
tests:
  - cmd: echo doc1
---
tests:
  - cmd: echo doc2, silently dropped before this fix
`)
	_, err := ParseFile(path)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "multiple YAML documents are not supported")
}

func TestParseFile_TraversalFileNamesRejected(t *testing.T) {
	// Non-local fixture names are rejected at parse time so `dats syntax`
	// catches them, not just the runner at fixture-setup time.
	cases := map[string]string{
		"inputs.files": `
tests:
  - cmd: echo hi
    inputs:
      files:
        ../evil.txt: pwned
`,
		"outputs.files": `
tests:
  - cmd: echo hi
    outputs:
      files:
        ../../evil.txt:
          exists: true
`,
		"outputs.!files": `
tests:
  - cmd: echo hi
    outputs:
      "!files":
        ../evil.txt:
          exists: true
`,
		"absolute path": `
tests:
  - cmd: echo hi
    inputs:
      files:
        /etc/evil.txt: pwned
`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseFile(writeTempDats(t, content))
			require.NotNil(t, err)
			assert.Contains(t, err.Error(), "must be a relative path that stays inside the test directory")
		})
	}
}

func TestParseFile_TraversalErrorNamesTestAndFile(t *testing.T) {
	path := writeTempDats(t, `
tests:
  - cmd: echo hi
  - cmd: echo hi
    inputs:
      files:
        ../evil.txt: pwned
`)
	_, err := ParseFile(path)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), `test 2: input file name "../evil.txt"`)
}

func TestParseFile_NestedLocalFileNamesAllowed(t *testing.T) {
	// Nested relative names like sub/file.txt are local and stay accepted.
	path := writeTempDats(t, `
tests:
  - cmd: cat {inputs.sub/dir/nested.txt}
    inputs:
      files:
        sub/dir/nested.txt: content
    outputs:
      files:
        sub/out.txt:
          exists: false
      "!files":
        other/missing.txt:
          exists: true
`)
	tf, err := ParseFile(path)
	require.Nil(t, err)
	assert.Equal(t, 1, len(tf.Tests))
}
