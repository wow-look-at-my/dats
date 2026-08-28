package schema

// Tests for the file-level ssh block: the shape it accepts, and the parse
// errors that stop a file smuggling an ssh option through the target.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseSSH(t *testing.T, body string) (*TestFile, error) {
	t.Helper()
	return ParseFile(writeTempDats(t, body+`
tests:
	- desc: t
	  cmd: echo hi
`))
}

func TestParseSSHAbsent(t *testing.T) {
	tf, err := parseSSH(t, "")
	require.Nil(t, err)
	assert.Nil(t, tf.SSH)
	assert.Equal(t, "", tf.SSH.TargetName())
}

func TestParseSSHTarget(t *testing.T) {
	tf, err := parseSSH(t, "ssh: build@box\n")
	require.Nil(t, err)
	require.NotNil(t, tf.SSH)
	assert.Equal(t, "build@box", tf.SSH.TargetName())
}

func TestParseSSHExplicitNullIsAbsent(t *testing.T) {
	tf, err := parseSSH(t, "ssh: null\n")
	require.Nil(t, err)
	assert.Nil(t, tf.SSH)
}

// TestParseSSHFalseIsRejected: local is the privileged side, so a file
// asking to run here is reaching for more, not waiving a protection.
func TestParseSSHFalseIsRejected(t *testing.T) {
	_, err := parseSSH(t, "ssh: false\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot move a command onto the machine running dats")
}

func TestParseSSHTrueIsRejected(t *testing.T) {
	_, err := parseSSH(t, "ssh: true\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--ssh")
}

// TestParseSSHRejectsAnOptionShapedTarget is a security test: ssh accepts no
// "--" before its target, so -oProxyCommand= would run a command on the
// machine that merely opened the file.
func TestParseSSHRejectsAnOptionShapedTarget(t *testing.T) {
	for _, target := range []string{
		"-oProxyCommand=touch /tmp/pwned",
		"-F/tmp/evil",
		"-",
	} {
		t.Run(target, func(t *testing.T) {
			_, err := parseSSH(t, "ssh: \""+target+"\"\n")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "dash")
		})
	}
}

func TestParseSSHRejectsAMalformedTarget(t *testing.T) {
	for _, target := range []string{"host evil", "host;id", "../host", "host$X"} {
		t.Run(target, func(t *testing.T) {
			_, err := parseSSH(t, "ssh: \""+target+"\"\n")
			require.Error(t, err)
		})
	}
}

func TestParseSSHRejectsANonString(t *testing.T) {
	_, err := parseSSH(t, "ssh:\n\thost: box\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be a target string")
}

// TestParseSSHRejectsAMatrixPlaceholder pins that the file-level target is
// resolved once, before any instance exists.
func TestParseSSHRejectsAMatrixPlaceholder(t *testing.T) {
	_, err := parseSSH(t, "ssh: \"{matrix.host}\"\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not available outside tests")
}
