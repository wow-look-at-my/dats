package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFile_NegatedOutputKeysUnquoted(t *testing.T) {
	path := writeTempDats(t, `
tests:
	- cmd: echo hi
	  outputs:
		!stdout:
			- boom
		!stderr:
			0: "^never$"
		!files:
			stray.txt:
				exists: true
`)
	tf, err := ParseFile(path)
	require.Nil(t, err)
	require.Equal(t, 1, len(tf.Tests))
	out := tf.Tests[0].Outputs
	assert.Equal(t, []string{"boom"}, out.NotStdout.Patterns)
	assert.Equal(t, map[int]string{0: "^never$"}, out.NotStderr.LineChecks)
	assert.Contains(t, out.NotFiles, "stray.txt")
}

func TestParseFile_NegatedOutputKeysQuotedMatchUnquoted(t *testing.T) {
	bare := writeTempDats(t, `
tests:
	- cmd: echo hi
	  outputs:
		!stdout:
			- boom
		!files:
			stray.txt: {}
`)
	quoted := writeTempDats(t, `
tests:
	- cmd: echo hi
	  outputs:
		"!stdout":
			- boom
		"!files":
			stray.txt: {}
`)
	a, err := ParseFile(bare)
	require.Nil(t, err)
	b, err := ParseFile(quoted)
	require.Nil(t, err)
	assert.Equal(t, a.Tests[0].Outputs, b.Tests[0].Outputs)
}
