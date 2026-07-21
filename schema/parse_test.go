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

func TestParseFile_SetupTeardownSharedRoundtrip(t *testing.T) {
	path := writeTempDats(t, `
shared:
  files:
    config.json: '{"debug": true}'
    sub/nested.txt: nested
setup:
  - mkdir output
  - cp {shared.config.json} output/
teardown: echo done
tests:
  - cmd: cat {shared.config.json}
`)
	tf, err := ParseFile(path)
	require.Nil(t, err)
	assert.Equal(t, SetupCommands{"mkdir output", "cp {shared.config.json} output/"}, tf.Setup)
	assert.Equal(t, TeardownCommands{"echo done"}, tf.Teardown)
	require.NotNil(t, tf.Shared)
	assert.Equal(t, map[string]string{
		"config.json":    `{"debug": true}`,
		"sub/nested.txt": "nested",
	}, tf.Shared.Files)
}

func TestParseFile_SetupStringForm(t *testing.T) {
	// A single scalar string is the one-command form of setup/teardown.
	path := writeTempDats(t, `
setup: echo one command
teardown:
  - echo first
  - echo second
tests:
  - cmd: true
`)
	tf, err := ParseFile(path)
	require.Nil(t, err)
	assert.Equal(t, SetupCommands{"echo one command"}, tf.Setup)
	assert.Equal(t, TeardownCommands{"echo first", "echo second"}, tf.Teardown)
}

func TestParseFile_WithoutNewKeysUnchanged(t *testing.T) {
	// Backwards compatibility: files without setup/teardown/shared parse with
	// all three absent.
	path := writeTempDats(t, `
tests:
  - cmd: echo hi
`)
	tf, err := ParseFile(path)
	require.Nil(t, err)
	assert.Nil(t, tf.Setup)
	assert.Nil(t, tf.Teardown)
	assert.Nil(t, tf.Shared)
}

func TestParseFile_CommandListRejected(t *testing.T) {
	cases := map[string]struct {
		content string
		wantErr string
	}{
		"setup empty list": {`
setup: []
tests:
  - cmd: true
`, "setup: must list at least one command"},
		"teardown empty list": {`
teardown: []
tests:
  - cmd: true
`, "teardown: must list at least one command"},
		"setup blank string": {`
setup: "   "
tests:
  - cmd: true
`, "setup: command must not be empty"},
		"setup numeric scalar": {`
setup: 42
tests:
  - cmd: true
`, "setup: command must be a string"},
		"setup blank element": {`
setup:
  - echo ok
  - "  "
tests:
  - cmd: true
`, "setup: command 2 must not be empty"},
		"setup sequence element": {`
setup:
  - echo ok
  - [not, a, string]
tests:
  - cmd: true
`, "setup: command 2 must be a string"},
		"setup numeric element": {`
setup:
  - echo ok
  - 42
tests:
  - cmd: true
`, "setup: command 2 must be a string"},
		"teardown map element": {`
teardown:
  - cmd: nope
tests:
  - cmd: true
`, "teardown: command 1 must be a string"},
		"setup map node": {`
setup:
  cmd: nope
tests:
  - cmd: true
`, "setup must be a command string or a list of command strings"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseFile(writeTempDats(t, tc.content))
			require.NotNil(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestParseFile_SharedRejected(t *testing.T) {
	cases := map[string]struct {
		content string
		wantErr string
	}{
		"no files key": {`
shared: {}
tests:
  - cmd: true
`, "shared: must declare at least one file under files"},
		"empty files": {`
shared:
  files: {}
tests:
  - cmd: true
`, "shared: must declare at least one file under files"},
		"traversal name": {`
shared:
  files:
    ../evil.txt: pwned
tests:
  - cmd: true
`, `shared file name "../evil.txt" must be a relative path that stays inside the shared directory`},
		"absolute name": {`
shared:
  files:
    /etc/evil.txt: pwned
tests:
  - cmd: true
`, "must be a relative path that stays inside the shared directory"},
		"unknown key under shared": {`
shared:
  files:
    a.txt: content
  bogus: true
tests:
  - cmd: true
`, "not found"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseFile(writeTempDats(t, tc.content))
			require.NotNil(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestParseFile_SnapshotForms(t *testing.T) {
	// Both accepted shapes parse through the full file path: the boolean
	// shorthand (stdout only) and the per-stream mapping. `snapshot: false`
	// is the documented toggle-off, identical to omitting the key.
	path := writeTempDats(t, `
tests:
  - desc: shorthand
    cmd: echo hi
    outputs:
      snapshot: true
  - desc: toggled off
    cmd: echo hi
    outputs:
      snapshot: false
  - desc: stderr only
    cmd: echo err >&2
    outputs:
      snapshot:
        stderr: true
`)
	tf, err := ParseFile(path)
	require.Nil(t, err)
	require.Equal(t, 3, len(tf.Tests))
	assert.Equal(t, SnapshotCheck{Enabled: true, Stdout: true}, tf.Tests[0].Outputs.Snapshot)
	assert.Equal(t, SnapshotCheck{}, tf.Tests[1].Outputs.Snapshot)
	assert.Equal(t, SnapshotCheck{Enabled: true, Stderr: true}, tf.Tests[2].Outputs.Snapshot)
}

func TestParseFile_SnapshotRejected(t *testing.T) {
	// The unmarshaler's errors surface through ParseFile, so `dats syntax`
	// catches a malformed snapshot key without running anything.
	cases := map[string]struct {
		content string
		wantErr string
	}{
		"unknown stream key": {`
tests:
  - cmd: echo hi
    outputs:
      snapshot:
        files: true
`, `snapshot: unknown key "files" (allowed: stdout, stderr)`},
		"nothing enabled": {`
tests:
  - cmd: echo hi
    outputs:
      snapshot: {}
`, "snapshot: must enable at least one of stdout, stderr"},
		"non-bool scalar": {`
tests:
  - cmd: echo hi
    outputs:
      snapshot: everything
`, "snapshot: must be true, false, or a mapping of stream booleans (stdout, stderr)"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseFile(writeTempDats(t, tc.content))
			require.NotNil(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
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
