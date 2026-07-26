package schema

// Tests for the file-level sandbox block: the two accepted shapes, the
// defaults an unstated key carries, and the parse errors that keep a
// misspelled key from silently disabling isolation.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseSandbox(t *testing.T, body string) (*TestFile, error) {
	t.Helper()
	return ParseFile(writeTempDats(t, body+`
tests:
  - desc: t
    cmd: echo hi
`))
}

func TestParseSandboxAbsent(t *testing.T) {
	tf, err := parseSandbox(t, "")
	require.Nil(t, err)
	assert.Nil(t, tf.Sandbox)
	// The nil spec is the "CLI decides" case, and its accessors say so.
	assert.True(t, tf.Sandbox.IsEnabled())
	assert.True(t, tf.Sandbox.NetworkEnabled())
}

func TestParseSandboxExplicitNullIsAbsent(t *testing.T) {
	tf, err := parseSandbox(t, "sandbox:\n")
	require.Nil(t, err)
	assert.Nil(t, tf.Sandbox)
}

func TestParseSandboxScalarBool(t *testing.T) {
	tf, err := parseSandbox(t, "sandbox: false\n")
	require.Nil(t, err)
	require.NotNil(t, tf.Sandbox)
	assert.False(t, tf.Sandbox.IsEnabled())

	tf, err = parseSandbox(t, "sandbox: true\n")
	require.Nil(t, err)
	require.NotNil(t, tf.Sandbox)
	assert.True(t, tf.Sandbox.IsEnabled())
	assert.True(t, tf.Sandbox.NetworkEnabled(), "an unstated network key keeps the network")
}

func TestParseSandboxMapping(t *testing.T) {
	tf, err := parseSandbox(t, `sandbox:
  enabled: true
  network: false
  image: alpine:3.20
  writable:
    - /var/data
    - relative/path
`)
	require.Nil(t, err)
	require.NotNil(t, tf.Sandbox)
	assert.True(t, tf.Sandbox.IsEnabled())
	assert.False(t, tf.Sandbox.NetworkEnabled())
	assert.Equal(t, "alpine:3.20", tf.Sandbox.Image)
	assert.Equal(t, []string{"/var/data", "relative/path"}, tf.Sandbox.Writable)
}

func TestParseSandboxErrors(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"unknown key", "sandbox:\n  enable: true\n", `unknown key "enable"`},
		{"duplicate key", "sandbox:\n  network: true\n  network: false\n", "network declared more than once"},
		{"non-bool enabled", "sandbox:\n  enabled: yes-please\n", "enabled must be a boolean"},
		{"non-bool network", "sandbox:\n  network: [1]\n", "network must be a boolean"},
		{"empty image", "sandbox:\n  image: \"\"\n", "image must be a non-empty string"},
		{"non-string image", "sandbox:\n  image: 42\n", "image must be a non-empty string"},
		{"empty writable", "sandbox:\n  writable: []\n", "writable must list at least one path"},
		{"non-sequence writable", "sandbox:\n  writable: /var/data\n", "writable must list at least one path"},
		{"empty writable entry", "sandbox:\n  writable:\n    - \"\"\n", "writable path 1 must be a non-empty string"},
		{"empty mapping", "sandbox: {}\n", "must set at least one of"},
		{"wrong kind", "sandbox:\n  - false\n", "must be true, false, or a mapping"},
		{"scalar not bool", "sandbox: maybe\n", "must be true, false, or a mapping"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseSandbox(t, tc.body)
			require.NotNil(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestParseSandboxRejectsMatrixPlaceholders(t *testing.T) {
	// The sandbox is resolved once per file, before any instance exists, so a
	// {matrix.X} there could never resolve -- and silently doing nothing is
	// the worst possible outcome for a security-relevant key.
	_, err := parseSandbox(t, "sandbox:\n  image: img:{matrix.tag}\n")
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "sandbox image: {matrix.tag} is not available outside tests")

	_, err = parseSandbox(t, "sandbox:\n  writable:\n    - /data/{matrix.name}\n")
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "sandbox writable path 1: {matrix.name} is not available outside tests")
}
