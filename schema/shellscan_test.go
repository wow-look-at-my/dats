package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFile_ScriptConstructsRejected(t *testing.T) {
	// A cmd is a command, not a script: each construct below has a schema key.
	cases := map[string]struct {
		content string
		wantErr string
	}{
		"semicolon": {`
tests:
	- cmd: echo one; echo two
`, "test 1: cmd: must not separate commands with `;`"},
		"and list": {`
tests:
	- cmd: echo one && echo two
`, "test 1: cmd: must not chain commands with `&&`"},
		"output redirect": {`
tests:
	- cmd: echo hi > out.txt
`, "test 1: cmd: must not redirect to a file"},
		"append redirect": {`
tests:
	- cmd: echo hi >> out.txt
`, "test 1: cmd: must not redirect to a file"},
		"input redirect": {`
tests:
	- cmd: sort < in.txt
`, "test 1: cmd: must not redirect to a file"},
		"stderr to a file": {`
tests:
	- cmd: make 2> errors.txt
`, "test 1: cmd: must not redirect to a file"},
		"cd": {`
tests:
	- cmd: cd build
`, "test 1: cmd: must not cd"},
		"cd in a setup hook": {`
setup: cd build
tests:
	- cmd: "true"
`, "setup: command: must not cd"},
		"semicolon in a teardown hook": {`
teardown: echo one; echo two
tests:
	- cmd: "true"
`, "teardown: command: must not separate commands with `;`"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseFile(writeTempDats(t, tc.content))
			require.NotNil(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestParseFile_ShellConstructsAccepted(t *testing.T) {
	// The bans are on what a command DOES, so an operator that is data, or that
	// keeps output inside dats, stays legal. A byte scanner got each of these
	// wrong, which is why the check parses instead.
	cases := map[string]string{
		"semicolon inside a sed script": `
tests:
	- cmd: sed 's/a/b/;s/c/d/' {inputs.in.txt}
	  inputs:
		files:
			in.txt: a
`,
		"greater-than inside arithmetic": `
tests:
	- cmd: echo $(( 3 > 2 ))
`,
		"cd as an argument": `
tests:
	- cmd: echo cd
`,
		"stderr onto stdout": `
tests:
	- cmd: echo hi >&2
`,
		"or list, the sanctioned way to expect a failure": `
tests:
	- cmd: false || echo recovered
`,
		"a pipeline": `
tests:
	- cmd: echo hi | tr a-z A-Z
`,
		"newline-separated commands in a block scalar": `
tests:
	- cmd: |
		echo one
		echo two
`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseFile(writeTempDats(t, content))
			assert.Nil(t, err)
		})
	}
}

func TestParseFile_WorkdirRejected(t *testing.T) {
	cases := map[string]struct {
		content string
		wantErr string
	}{
		"absolute": {`
workdir: /etc
tests:
	- cmd: "true"
`, `workdir: "/etc" must be a relative path`},
		"climbs out": {`
workdir: ../elsewhere
tests:
	- cmd: "true"
`, `workdir: "../elsewhere" must be a relative path`},
		"per-test absolute": {`
tests:
	- cmd: "true"
	  workdir: /etc
`, `test 1: workdir: "/etc" must be a relative path`},
		"matrix reference at file level": {`
workdir: "{matrix.dir}"
tests:
	- cmd: "true"
`, "workdir: {matrix.dir} is not available outside tests"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseFile(writeTempDats(t, tc.content))
			require.NotNil(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestParseFile_WorkdirAccepted(t *testing.T) {
	tf, err := ParseFile(writeTempDats(t, `
workdir: sub
tests:
	- cmd: "true"

	- cmd: "true"
	  workdir: other/deeper
`))
	require.NoError(t, err)
	assert.Equal(t, "sub", tf.Workdir)
	assert.Equal(t, "", tf.Tests[0].Workdir)
	assert.Equal(t, "other/deeper", tf.Tests[1].Workdir)
}
