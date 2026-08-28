package schema


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
	// The nil spec is the "nothing narrowed" case, and its accessor says so.
	assert.True(t, tf.Sandbox.NetworkEnabled())
}

func TestParseSandboxExplicitNullIsAbsent(t *testing.T) {
	tf, err := parseSandbox(t, "sandbox: null\n")
	require.Nil(t, err)
	assert.Nil(t, tf.Sandbox)
}

func TestParseSandboxMapping(t *testing.T) {
	tf, err := parseSandbox(t, `sandbox:
	network: false
	image: alpine:3.20
`)
	require.Nil(t, err)
	require.NotNil(t, tf.Sandbox)
	assert.False(t, tf.Sandbox.NetworkEnabled())
	assert.Equal(t, "alpine:3.20", tf.Sandbox.Image)
}

func TestParseSandboxCannotDisableItself(t *testing.T) {
	for _, body := range []string{
		"sandbox: false\n",
		"sandbox:\n\tenabled: false\n",
		"sandbox:\n\tenabled: false\n\tnetwork: false\n",
	} {
		_, err := parseSandbox(t, body)
		require.NotNil(t, err, "body: %q", body)
		assert.Contains(t, err.Error(), "a file cannot turn its own sandbox off")
		assert.Contains(t, err.Error(), "--no-sandbox")
	}
}

func TestParseSandboxRejectsStatingTheDefault(t *testing.T) {
	for _, body := range []string{"sandbox: true\n", "sandbox:\n\tenabled: true\n"} {
		_, err := parseSandbox(t, body)
		require.NotNil(t, err, "body: %q", body)
		assert.Contains(t, err.Error(), "commands are sandboxed unless the run opts out")
	}
}

func TestParseSandboxRejectsWritableKey(t *testing.T) {
	_, err := parseSandbox(t, "sandbox:\n\twritable:\n\t\t- /var/data\n")
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), `unknown key "writable"`)
	assert.Contains(t, err.Error(), "allowed: network, image")
}

func TestParseSandboxErrors(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"unknown key", "sandbox:\n\tenable: true\n", `unknown key "enable"`},
		{"duplicate key", "sandbox:\n\tnetwork: true\n\tnetwork: false\n", `duplicate mapping key "network"`},
		{"non-bool enabled", "sandbox:\n\tenabled: yes-please\n", "a file cannot turn its own sandbox off"},
		{"non-bool network", "sandbox:\n\tnetwork:\n\t\t- 1\n", "network must be a boolean"},
		{"empty image", "sandbox:\n\timage: \"\"\n", "image must be a non-empty string"},
		{"non-string image", "sandbox:\n\timage: 42\n", "image must be a non-empty string"},
		{"empty mapping", "sandbox:\n\t{}\n", "must set at least one of"},
		{"wrong kind", "sandbox:\n\t- false\n", "must be a mapping"},
		{"scalar not bool", "sandbox: maybe\n", "must be a mapping"},
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
	_, err := parseSandbox(t, "sandbox:\n\timage: img:{matrix.tag}\n")
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "sandbox image: {matrix.tag} is not available outside tests")
}
