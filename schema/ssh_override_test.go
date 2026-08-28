package schema

// Tests for the per-test ssh override: when it is legal, and that matrix
// expansion gives each instance its own target rather than a shared one.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTestSSHNeedsAFileLevelTarget(t *testing.T) {
	_, err := ParseFile(writeTempDats(t, `tests:
	- desc: t
	  cmd: echo hi
	  ssh: other@box
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "needs a file-level ssh: target too")
}

func TestParseTestSSHWithAFileLevelTarget(t *testing.T) {
	tf, err := ParseFile(writeTempDats(t, `ssh: home@box
tests:
	- desc: on the file's host
	  cmd: echo hi
	- desc: overridden
	  cmd: echo hi
	  ssh: other@box
`))
	require.Nil(t, err)
	assert.Equal(t, "home@box", tf.SSH.TargetName())
	assert.Nil(t, tf.Tests[0].SSH)
	assert.Equal(t, "other@box", tf.Tests[1].SSH.TargetName())
}

func TestParseTestSSHFalseIsRejected(t *testing.T) {
	_, err := ParseFile(writeTempDats(t, `ssh: home@box
tests:
	- desc: t
	  cmd: echo hi
	  ssh: false
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot move a command onto the machine running dats")
}

// TestMatrixSubstitutesThePerTestTarget is the feature's best demo: one test
// fanned across a fleet. The file-level target is out of scope for the
// opposite reason -- it resolves once, before any instance exists.
func TestMatrixSubstitutesThePerTestTarget(t *testing.T) {
	tf, err := ParseFile(writeTempDats(t, `ssh: home@box
tests:
	- desc: on {matrix.host}
	  cmd: echo hi
	  ssh: "{matrix.host}"
	  matrix:
		host: [alpha, beta]
`))
	require.Nil(t, err)

	instances := ExpandMatrix(&tf.Tests[0])
	require.Len(t, instances, 2)
	assert.Equal(t, "alpha", instances[0].Test.SSH.TargetName())
	assert.Equal(t, "beta", instances[1].Test.SSH.TargetName())

	// The SSH field is a pointer: a shallow copy would make every instance
	// share one struct, so substituting into one would corrupt its siblings.
	assert.NotSame(t, instances[0].Test.SSH, instances[1].Test.SSH)
	assert.Equal(t, "{matrix.host}", tf.Tests[0].SSH.TargetName(), "the template must be untouched")
}

func TestMatrixRejectsAnUndeclaredReferenceInATarget(t *testing.T) {
	_, err := ParseFile(writeTempDats(t, `ssh: home@box
tests:
	- desc: t
	  cmd: echo hi
	  ssh: "{matrix.nope}"
	  matrix:
		host: [alpha]
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nope")
}
