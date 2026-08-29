package runner

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wow-look-at-my/dats/schema"
)

func TestSSHManagerNilResolvesLocal(t *testing.T) {
	var m *SSHManager
	c, refused, err := m.Resolve("a.dats", &schema.SSHSpec{Target: "build@box"})
	require.NoError(t, err)
	assert.Nil(t, c, "a run with no ssh policy must stay local whatever a file says")
	assert.Empty(t, refused)
}

func TestSSHManagerNoTargetAnywhereIsLocal(t *testing.T) {
	m := &SSHManager{Allow: func(string, string) error { return nil }}
	c, _, err := m.Resolve("a.dats", nil)
	require.NoError(t, err)
	assert.Nil(t, c)
}

func TestSSHManagerTypedTargetOutranksTheFile(t *testing.T) {
	m := &SSHManager{Target: "typed@box"}
	c, refused, err := m.Resolve("a.dats", &schema.SSHSpec{Target: "file@box"})
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, "typed@box", c.Target)
	assert.Equal(t, "file@box", refused)
}

func TestSSHManagerTypedTargetMatchingTheFileRefusesNothing(t *testing.T) {
	m := &SSHManager{Target: "same@box"}
	_, refused, err := m.Resolve("a.dats", &schema.SSHSpec{Target: "same@box"})
	require.NoError(t, err)
	assert.Empty(t, refused)
}

func TestSSHManagerRefusesAFileTargetWithoutAnApprover(t *testing.T) {
	m := &SSHManager{}
	_, _, err := m.Resolve("a.dats", &schema.SSHSpec{Target: "file@box"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not approved")
	assert.Contains(t, err.Error(), "dats trust add")
}

func TestSSHManagerUsesAnApprovedFileTarget(t *testing.T) {
	var askedFile, askedTarget string
	m := &SSHManager{Allow: func(f, target string) error {
		askedFile, askedTarget = f, target
		return nil
	}}
	c, _, err := m.Resolve("a.dats", &schema.SSHSpec{Target: "file@box"})
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, "file@box", c.Target)
	assert.Equal(t, "a.dats", askedFile)
	assert.Equal(t, "file@box", askedTarget)
}

func TestSSHManagerPropagatesARefusal(t *testing.T) {
	refusal := errors.New("not approved by policy")
	m := &SSHManager{Allow: func(string, string) error { return refusal }}
	_, _, err := m.Resolve("a.dats", &schema.SSHSpec{Target: "file@box"})
	require.ErrorIs(t, err, refusal)
}

// TestSSHManagerNeverAsksAboutATypedTarget: typing it IS the approval.
func TestSSHManagerNeverAsksAboutATypedTarget(t *testing.T) {
	asked := false
	m := &SSHManager{Target: "typed@box", Allow: func(string, string) error {
		asked = true
		return errors.New("should not be consulted")
	}}
	_, _, err := m.Resolve("a.dats", nil)
	require.NoError(t, err)
	assert.False(t, asked)
}

func TestSSHManagerReusesOneConnectionPerTarget(t *testing.T) {
	m := &SSHManager{Target: "build@box"}
	first, _, err := m.Resolve("a.dats", nil)
	require.NoError(t, err)
	second, _, err := m.Resolve("b.dats", nil)
	require.NoError(t, err)
	assert.Same(t, first, second)
	m.Close()
}

func TestSSHManagerValidatesAFileTarget(t *testing.T) {
	m := &SSHManager{Allow: func(string, string) error { return nil }}
	_, _, err := m.Resolve("a.dats", &schema.SSHSpec{Target: "-oProxyCommand=id"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dash")
}

func TestSSHManagerCloseIsSafeWhenNothingConnected(t *testing.T) {
	assert.NotPanics(t, func() { (&SSHManager{}).Close() })
	var m *SSHManager
	assert.NotPanics(t, m.Close)
}
