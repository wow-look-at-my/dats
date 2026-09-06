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
			- hi
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
		- typo of stdout at the wrong level
`,
		"outputs level": `
tests:
	- cmd: echo hi
	  outputs:
		stdotu:
			- typo of stdout
`,
		"inputs level": `
tests:
	- cmd: echo hi
	  inputs:
		file:
			a.txt: typo of files
`,
		"file check level": `
tests:
	- cmd: echo hi
	  outputs:
		files:
			out.txt:
				matches:
					- typo of match
`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseFile(writeTempDats(t, content))
			require.NotNil(t, err)
			assert.Contains(t, err.Error(), "unknown field")
		})
	}
}

func TestParseFile_SchemaKeyAllowed(t *testing.T) {
	path := writeTempDats(t, `
$schema: https://github.com/wow-look-at-my/dats/schema.json
tests:
	- cmd: echo hi
`)
	tf, err := ParseFile(path)
	require.Nil(t, err)
	assert.Equal(t, 1, len(tf.Tests))
}

func TestParseFile_InputEnvAccepted(t *testing.T) {
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
	// A later "---" document used to be silently ignored, dropping its tests.
	path := writeTempDats(t, `
tests:
	- cmd: echo doc1
---
tests:
	- cmd: echo doc2, silently dropped before this fix
`)
	_, err := ParseFile(path)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "input contains 2 documents")
}

func TestParseFile_TraversalFileNamesRejected(t *testing.T) {
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
		!files:
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
		config.json: "{\"debug\": true}"
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
	assert.Equal(t, SetupCommands{{Cmd: "mkdir output"}, {Cmd: "cp {shared.config.json} output/"}}, tf.Setup)
	assert.Equal(t, TeardownCommands{{Cmd: "echo done"}}, tf.Teardown)
	require.NotNil(t, tf.Shared)
	assert.Equal(t, map[string]string{
		"config.json":    `{"debug": true}`,
		"sub/nested.txt": "nested",
	}, tf.Shared.Files)
}

func TestParseFile_SetupStringForm(t *testing.T) {
	// A single scalar string is the single-command form of setup/teardown.
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
	assert.Equal(t, SetupCommands{{Cmd: "echo one command"}}, tf.Setup)
	assert.Equal(t, TeardownCommands{{Cmd: "echo first"}, {Cmd: "echo second"}}, tf.Teardown)
}

func TestParseFile_WithoutNewKeysUnchanged(t *testing.T) {
	// Backwards compatibility: files without setup/teardown/shared parse with every key absent.
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
	-
		- not
		- a
		- string
tests:
	- cmd: true
`, "setup: command 2 must be a command string or a mapping"},
		"setup numeric element": {`
setup:
	- echo ok
	- 42
tests:
	- cmd: true
`, "setup: command 2 must be a string"},
		"teardown map element unknown key": {`
teardown:
	- foo: nope
tests:
	- cmd: true
`, `teardown: command 1: unknown key "foo"`},
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
shared:
	{}
tests:
	- cmd: true
`, "shared: must declare at least one file under files"},
		"empty files": {`
shared:
	files:
		{}
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
`, "unknown field"},
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
		snapshot:
			{}
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

func TestParseFile_EmptyOutputAssertionRejected(t *testing.T) {
	// An outputs key that names a stream and checks nothing reports ok while the
	// reader believes the output was verified. Every spelling of it is rejected.
	cases := map[string]struct {
		content string
		wantErr string
	}{
		"stdout empty list": {`
tests:
	- cmd: echo hi
	  outputs:
		stdout: []
`, "test 1: outputs.stdout: an empty check asserts nothing -- write a pattern, or drop the key"},
		"stdout empty map": {`
tests:
	- cmd: echo hi
	  outputs:
		stdout: {}
`, "test 1: outputs.stdout: an empty check asserts nothing"},
		"stdout explicit null": {`
tests:
	- cmd: echo hi
	  outputs:
		stdout:
`, "test 1: outputs.stdout: an empty check asserts nothing"},
		"stderr empty list": {`
tests:
	- cmd: echo hi
	  outputs:
		stderr: []
`, "test 1: outputs.stderr: an empty check asserts nothing"},
		"negated stdout empty list": {`
tests:
	- cmd: echo hi
	  outputs:
		!stdout: []
`, "test 1: outputs.!stdout: an empty check asserts nothing"},
		"negated stderr empty list": {`
tests:
	- cmd: echo hi
	  outputs:
		!stderr: []
`, "test 1: outputs.!stderr: an empty check asserts nothing"},
		"files empty map": {`
tests:
	- cmd: echo hi
	  outputs:
		files: {}
`, "test 1: outputs.files: an empty mapping asserts nothing -- name a file, or drop the key"},
		"negated files empty map": {`
tests:
	- cmd: echo hi
	  outputs:
		!files: {}
`, "test 1: outputs.!files: an empty mapping asserts nothing"},
		"error names the offending test": {`
tests:
	- cmd: echo hi
	  outputs:
		stdout:
			- hi

	- cmd: echo bye
	  outputs:
		stderr: []
`, "test 2: outputs.stderr: an empty check asserts nothing"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseFile(writeTempDats(t, tc.content))
			require.NotNil(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestParseFile_OutputAssertionsWithContentAccepted(t *testing.T) {
	// The rejection is about an EMPTY check, so a populated check and an absent
	// key both keep parsing. A bare cmd still checks the exit code.
	path := writeTempDats(t, `
tests:
	- cmd: echo hi

	- cmd: echo hi
	  outputs:
		stdout:
			- hi
		!stderr:
			- boom
		files:
			out.txt: {}
`)
	tf, err := ParseFile(path)
	require.NoError(t, err)
	require.Len(t, tf.Tests, 2)
	assert.True(t, tf.Tests[0].Outputs.Stdout.IsEmpty())
	assert.False(t, tf.Tests[0].Outputs.Stdout.Stated)
	assert.Equal(t, []string{"hi"}, tf.Tests[1].Outputs.Stdout.Patterns)
	assert.Equal(t, []string{"boom"}, tf.Tests[1].Outputs.NotStderr.Patterns)
	assert.Len(t, tf.Tests[1].Outputs.Files, 1)
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
		!files:
			other/missing.txt:
				exists: true
`)
	tf, err := ParseFile(path)
	require.Nil(t, err)
	assert.Equal(t, 1, len(tf.Tests))
}

func TestParseFile_CopyAccepted(t *testing.T) {
	path := writeTempDats(t, `
shared:
	copy:
		fixture.bin: ../fixtures/fixture.bin
tests:
	- cmd: cat {inputs.data.txt}
	  inputs:
		copy:
			data.txt: testdata/data.txt
`)
	tf, err := ParseFile(path)
	require.Nil(t, err)
	require.Equal(t, "../fixtures/fixture.bin", tf.Shared.Copy["fixture.bin"])
	require.Equal(t, "testdata/data.txt", tf.Tests[0].Inputs.Copy["data.txt"])
}

func TestParseFile_CopyRejected(t *testing.T) {
	cases := map[string]struct {
		content string
		wantErr string
	}{
		"shared copy traversal name": {`
shared:
	copy:
		../evil.txt: some/source.txt
tests:
	- cmd: true
`, `copy destination "../evil.txt" must be a relative path`},
		"shared copy absolute name": {`
shared:
	copy:
		/etc/evil.txt: some/source.txt
tests:
	- cmd: true
`, `copy destination "/etc/evil.txt" must be a relative path`},
		"shared copy empty source": {`
shared:
	copy:
		dest.txt: ""
tests:
	- cmd: true
`, `copy destination "dest.txt" must name a non-empty source path`},
		"shared name in both files and copy": {`
shared:
	files:
		dup.txt: content
	copy:
		dup.txt: some/source.txt
tests:
	- cmd: true
`, `"dup.txt" is declared under both files and copy`},
		"shared block with only copy is not empty": {`
shared:
	copy:
		{}
tests:
	- cmd: true
`, "shared: must declare at least one file under files or copy"},
		"inputs copy traversal name": {`
tests:
	- cmd: true
	  inputs:
		copy:
			../evil.txt: some/source.txt
`, `copy destination "../evil.txt" must be a relative path`},
		"inputs copy empty source": {`
tests:
	- cmd: true
	  inputs:
		copy:
			dest.txt: ""
`, `copy destination "dest.txt" must name a non-empty source path`},
		"inputs name in both files and copy": {`
tests:
	- cmd: true
	  inputs:
		files:
			dup.txt: content
		copy:
			dup.txt: some/source.txt
`, `"dup.txt" is declared under both files and copy`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseFile(writeTempDats(t, tc.content))
			require.NotNil(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestParseFile_HeredocRejected(t *testing.T) {
	cases := map[string]struct {
		content string
		wantErr string
	}{
		"cmd": {`
tests:
	- cmd: "cat <<EOF\nhello\nEOF\n"
`, "test 1: cmd: must not use a shell heredoc"},
		"setup": {`
setup: "cat <<-EOF\nhi\nEOF\n"
tests:
	- cmd: true
`, "setup: command: must not use a shell heredoc"},
		"teardown": {`
teardown:
	- "cat <<~EOF\nhi\nEOF"
tests:
	- cmd: true
`, "teardown: command 1: must not use a shell heredoc"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseFile(writeTempDats(t, tc.content))
			require.NotNil(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
			assert.Contains(t, err.Error(), "inputs.files/inputs.copy or shared.files/shared.copy")
		})
	}
}

func TestParseFile_HerestringRejected(t *testing.T) {
	cases := map[string]struct {
		content string
		wantErr string
	}{
		"cmd": {`
tests:
	- cmd: cat <<< "hello"
`, "test 1: cmd: must not use a shell herestring"},
		"setup": {`
setup: cat <<< "hello"
tests:
	- cmd: true
`, "setup: command: must not use a shell herestring"},
		"teardown": {`
teardown: cat <<< "hello"
tests:
	- cmd: true
`, "teardown: command: must not use a shell herestring"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseFile(writeTempDats(t, tc.content))
			require.NotNil(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
			assert.Contains(t, err.Error(), "inputs.stdin")
		})
	}
}

func TestParseFile_HeredocVsHerestringDistinguished(t *testing.T) {
	// A bare "<<" with a trailing "<" is a herestring; without it, a heredoc.
	heredocPath := writeTempDats(t, "tests:\n\t- cmd: \"cat <<EOF\\nhi\\nEOF\\n\"\n")
	_, err := ParseFile(heredocPath)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "must not use a shell heredoc")
	assert.NotContains(t, err.Error(), "herestring")

	herestringPath := writeTempDats(t, `
tests:
	- cmd: cat <<< "hi"
`)
	_, err = ParseFile(herestringPath)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "must not use a shell herestring")
	assert.NotContains(t, err.Error(), "heredoc")
}
