package runner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireSSH skips unless a target is configured AND usable: a second
// machine is not a reasonable prerequisite for every dev box. CI provisions
// loopback sshd and runs this same probe as its own step, so a skip can
// never be how CI reports that remote execution works.
func requireSSH(t *testing.T) *SSHManager {
	t.Helper()
	target := os.Getenv("DATS_TEST_SSH_TARGET")
	if target == "" {
		t.Skip("DATS_TEST_SSH_TARGET not set")
	}
	m := &SSHManager{Target: target}
	c, _, err := m.Resolve("probe.dats", nil)
	require.NoError(t, err)
	if err := c.Connect(context.Background()); err != nil {
		m.Close()
		t.Skipf("ssh target %s not usable here: %v", target, err)
	}
	t.Cleanup(m.Close)
	return m
}

// writeSuite writes body to a .dats file in dir and returns its path.
func writeSuite(t *testing.T, dir, body string) string {
	t.Helper()
	suite := filepath.Join(dir, "suite.dats")
	require.NoError(t, os.WriteFile(suite, []byte(body), 0o644))
	return suite
}

// runRemoteSuite runs body on the target and returns the result plus output.
func runRemoteSuite(t *testing.T, ssh *SSHManager, body string) (*FileResult, string) {
	t.Helper()
	var out bytes.Buffer
	r := NewRunner(&out, false, false, "")
	r.SSH = ssh
	res, err := r.RunFile(context.Background(), writeSuite(t, t.TempDir(), body))
	require.NoError(t, err, "output:\n%s", out.String())
	return res, out.String()
}

// TestSSHRunsCommandsOnTheTarget is the end-to-end proof: the command runs
// over ssh, its fixtures arrive, and its output files come home.
func TestSSHRunsCommandsOnTheTarget(t *testing.T) {
	ssh := requireSSH(t)
	res, out := runRemoteSuite(t, ssh, "tests:\n"+
		"\t- desc: fixtures arrive and outputs come home\n"+
		"\t  cmd: cat {inputs.in.txt} > {outputs.out.txt}; echo done\n"+
		"\t  inputs:\n"+
		"\t\tfiles:\n"+
		"\t\t\tin.txt: hello remote\n"+
		"\t  outputs:\n"+
		"\t\tstdout:\n"+
		"\t\t\t- done\n"+
		"\t\tfiles:\n"+
		"\t\t\tout.txt:\n"+
		"\t\t\t\tmatch:\n"+
		"\t\t\t\t\t- hello remote\n")

	require.True(t, res.Ok(), "output:\n%s", out)
	assert.Contains(t, out, "ssh", "the header must announce where commands ran")
}

// TestSSHRunsOnTheOtherMachine proves the command really went through the
// transport rather than falling back to running here.
func TestSSHRunsOnTheOtherMachine(t *testing.T) {
	ssh := requireSSH(t)
	res, out := runRemoteSuite(t, ssh, "tests:\n"+
		"\t- desc: the command runs through ssh\n"+
		"\t  cmd: echo ran-on-$(uname -s)\n"+
		"\t  outputs:\n"+
		"\t\tstdout:\n"+
		"\t\t\t- ran-on-\n")
	require.True(t, res.Ok(), "output:\n%s", out)
}

func TestSSHCarriesSharedFixturesAndHooks(t *testing.T) {
	ssh := requireSSH(t)
	res, out := runRemoteSuite(t, ssh, "shared:\n"+
		"\tfiles:\n"+
		"\t\tcfg.txt: shared-value\n"+
		"setup: test -f {shared.cfg.txt}\n"+
		"teardown: test -f {shared.cfg.txt}\n"+
		"tests:\n"+
		"\t- desc: a test reads the shared fixture\n"+
		"\t  cmd: cat {shared.cfg.txt}\n"+
		"\t  outputs:\n"+
		"\t\tstdout:\n"+
		"\t\t\t- shared-value\n")
	require.True(t, res.Ok(), "output:\n%s", out)
}

func TestSSHCarriesEnvAndStdin(t *testing.T) {
	ssh := requireSSH(t)
	res, out := runRemoteSuite(t, ssh, "tests:\n"+
		"\t- desc: env and stdin reach the remote command\n"+
		"\t  cmd: cat; echo env=$MY_VAR\n"+
		"\t  inputs:\n"+
		"\t\tstdin: piped-text\n"+
		"\t\tenv:\n"+
		"\t\t\tMY_VAR: my-value\n"+
		"\t  outputs:\n"+
		"\t\tstdout:\n"+
		"\t\t\t- piped-text\n"+
		"\t\t\t- env=my-value\n")
	require.True(t, res.Ok(), "output:\n%s", out)
}

// TestSSHKeepsStderrSeparateFromStdout is what -T buys: a pty would merge
// the two streams and rewrite newlines, breaking every stderr assertion and
// every byte-exact golden.
func TestSSHKeepsStderrSeparateFromStdout(t *testing.T) {
	ssh := requireSSH(t)
	res, out := runRemoteSuite(t, ssh, "tests:\n"+
		"\t- desc: streams stay separate\n"+
		"\t  cmd: echo to-stdout; echo to-stderr >&2\n"+
		"\t  outputs:\n"+
		"\t\tstdout:\n"+
		"\t\t\t- to-stdout\n"+
		"\t\t!stdout:\n"+
		"\t\t\t- to-stderr\n"+
		"\t\tstderr:\n"+
		"\t\t\t- to-stderr\n")
	require.True(t, res.Ok(), "output:\n%s", out)
}

func TestSSHSurfacesARemoteExitCode(t *testing.T) {
	ssh := requireSSH(t)
	res, out := runRemoteSuite(t, ssh, "tests:\n"+
		"\t- desc: a remote exit code comes back\n"+
		"\t  cmd: exit 3\n"+
		"\t  exit: 3\n")
	require.True(t, res.Ok(), "output:\n%s", out)
}

// TestSSHCopyFixtureKeepsItsExecutableBit pins end to end the one property a
// plain file copy would lose.
func TestSSHCopyFixtureKeepsItsExecutableBit(t *testing.T) {
	ssh := requireSSH(t)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hello.sh"),
		[]byte("#!/bin/sh\necho from-script\n"), 0o755))
	suite := writeSuite(t, dir, "tests:\n"+
		"\t- desc: a copied script stays executable\n"+
		"\t  cmd: \"{inputs.hello.sh}\"\n"+
		"\t  inputs:\n"+
		"\t\tcopy:\n"+
		"\t\t\thello.sh: hello.sh\n"+
		"\t  outputs:\n"+
		"\t\tstdout:\n"+
		"\t\t\t- from-script\n")

	var out bytes.Buffer
	r := NewRunner(&out, false, false, "")
	r.SSH = ssh
	res, err := r.RunFile(context.Background(), suite)
	require.NoError(t, err)
	require.True(t, res.Ok(), "output:\n%s", out.String())
}

// TestSSHNormalizesRemotePathsInSnapshots keeps golden files portable: a
// remote command prints remote paths, and the goldens must come out
// byte-identical to a local run's.
func TestSSHNormalizesRemotePathsInSnapshots(t *testing.T) {
	ssh := requireSSH(t)
	suite := writeSuite(t, t.TempDir(), "tests:\n"+
		"\t- desc: paths normalize\n"+
		"\t  cmd: echo out={outputs.o.txt}\n"+
		"\t  outputs:\n"+
		"\t\tsnapshot: true\n")

	var out bytes.Buffer
	r := NewRunner(&out, false, false, "")
	r.SSH = ssh
	r.Update = true
	res, err := r.RunFile(context.Background(), suite)
	require.NoError(t, err)
	require.True(t, res.Ok(), "output:\n%s", out.String())

	golden, err := os.ReadFile(filepath.Join(SnapshotDir(suite), "001-paths-normalize.stdout.golden"))
	require.NoError(t, err)
	assert.Equal(t, "out={testdir}/outputs/o.txt\n", string(golden),
		"a remote path must normalize to the same token a local one does")
}
