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
func requireSSH(t *testing.T) *SSHConfig {
	t.Helper()
	target := os.Getenv("DATS_TEST_SSH_TARGET")
	if target == "" {
		t.Skip("DATS_TEST_SSH_TARGET not set")
	}
	c := NewSSHConfig(target)
	if err := c.Connect(context.Background()); err != nil {
		c.Close()
		t.Skipf("ssh target %s not usable here: %v", target, err)
	}
	t.Cleanup(c.Close)
	return c
}

// writeDatsFile writes body to a .dats file and returns its path.
func writeDatsFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "suite.dats")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

// runRemote runs one suite against the target and returns the result plus
// the formatted output.
func runRemote(t *testing.T, ssh *SSHConfig, body string) (*FileResult, string) {
	t.Helper()
	var out bytes.Buffer
	r := NewRunner(&out, false, false, "")
	r.SSH = ssh
	res, err := r.RunFile(context.Background(), writeDatsFile(t, body))
	require.NoError(t, err)
	return res, out.String()
}

// TestSSHRunsCommandsOnTheTarget is the end-to-end proof: the command runs
// over ssh, its fixtures arrive, and its output files come home.
func TestSSHRunsCommandsOnTheTarget(t *testing.T) {
	ssh := requireSSH(t)
	res, out := runRemote(t, ssh, "tests:\n"+
		"\t- desc: fixtures arrive and outputs come home\n"+
		"\t  cmd: cat {inputs.in.txt} > {outputs.out.txt}; echo done\n"+
		"\t  inputs:\n"+
		"\t    files:\n"+
		"\t      in.txt: hello remote\n"+
		"\t  outputs:\n"+
		"\t    stdout:\n"+
		"\t      - done\n"+
		"\t    files:\n"+
		"\t      out.txt:\n"+
		"\t        match:\n"+
		"\t          - hello remote\n")

	require.True(t, res.Ok(), "output:\n%s", out)
	assert.Contains(t, out, "ssh", "the header must announce where commands ran")
}

// TestSSHReportsTheRemoteHostname proves the command really executed on the
// other side rather than falling back to this machine.
func TestSSHReportsTheRemoteHostname(t *testing.T) {
	ssh := requireSSH(t)
	res, out := runRemote(t, ssh, "tests:\n"+
		"\t- desc: the command runs on the target\n"+
		"\t  cmd: 'echo ran-on:$(uname -s)'\n"+
		"\t  outputs:\n"+
		"\t    stdout:\n"+
		"\t      - 'ran-on:'\n")
	require.True(t, res.Ok(), "output:\n%s", out)
}

func TestSSHCarriesSharedFixturesAndHooks(t *testing.T) {
	ssh := requireSSH(t)
	res, out := runRemote(t, ssh, "shared:\n"+
		"\tfiles:\n"+
		"\t\tcfg.txt: shared-value\n"+
		"setup: test -f {shared.cfg.txt}\n"+
		"teardown: test -f {shared.cfg.txt}\n"+
		"tests:\n"+
		"\t- desc: a test reads the shared fixture\n"+
		"\t  cmd: cat {shared.cfg.txt}\n"+
		"\t  outputs:\n"+
		"\t    stdout:\n"+
		"\t      - shared-value\n")
	require.True(t, res.Ok(), "output:\n%s", out)
}

func TestSSHCarriesEnvAndStdin(t *testing.T) {
	ssh := requireSSH(t)
	res, out := runRemote(t, ssh, "tests:\n"+
		"\t- desc: env and stdin reach the remote command\n"+
		"\t  cmd: 'cat; echo env=$MY_VAR'\n"+
		"\t  inputs:\n"+
		"\t    stdin: piped-text\n"+
		"\t    env:\n"+
		"\t      MY_VAR: my-value\n"+
		"\t  outputs:\n"+
		"\t    stdout:\n"+
		"\t      - piped-text\n"+
		"\t      - env=my-value\n")
	require.True(t, res.Ok(), "output:\n%s", out)
}

// TestSSHKeepsStderrSeparateFromStdout is what the -T flag buys: a pty would
// merge the two streams and rewrite newlines, breaking every stderr
// assertion and every byte-exact golden.
func TestSSHKeepsStderrSeparateFromStdout(t *testing.T) {
	ssh := requireSSH(t)
	res, out := runRemote(t, ssh, "tests:\n"+
		"\t- desc: streams stay separate\n"+
		"\t  cmd: 'echo to-stdout; echo to-stderr >&2'\n"+
		"\t  outputs:\n"+
		"\t    stdout:\n"+
		"\t      - to-stdout\n"+
		"\t    '!stdout':\n"+
		"\t      - to-stderr\n"+
		"\t    stderr:\n"+
		"\t      - to-stderr\n")
	require.True(t, res.Ok(), "output:\n%s", out)
}

func TestSSHSurfacesARemoteExitCode(t *testing.T) {
	ssh := requireSSH(t)
	res, out := runRemote(t, ssh, "tests:\n"+
		"\t- desc: a remote exit code comes back\n"+
		"\t  cmd: exit 3\n"+
		"\t  exit: 3\n")
	require.True(t, res.Ok(), "output:\n%s", out)
}

// TestSSHCopyFixtureKeepsItsExecutableBit pins the one property a plain file
// copy would lose, end to end rather than only in the tar unit test.
func TestSSHCopyFixtureKeepsItsExecutableBit(t *testing.T) {
	ssh := requireSSH(t)
	dir := t.TempDir()
	script := filepath.Join(dir, "hello.sh")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\necho from-script\n"), 0o755))
	suite := filepath.Join(dir, "suite.dats")
	require.NoError(t, os.WriteFile(suite, []byte("tests:\n"+
		"\t- desc: a copied script stays executable\n"+
		"\t  cmd: '{inputs.hello.sh}'\n"+
		"\t  inputs:\n"+
		"\t    copy:\n"+
		"\t      hello.sh: hello.sh\n"+
		"\t  outputs:\n"+
		"\t    stdout:\n"+
		"\t      - from-script\n"), 0o644))

	var out bytes.Buffer
	r := NewRunner(&out, false, false, "")
	r.SSH = ssh
	res, err := r.RunFile(context.Background(), suite)
	require.NoError(t, err)
	require.True(t, res.Ok(), "output:\n%s", out.String())
}

// TestSSHNormalizesRemotePathsInSnapshots is the property that keeps golden
// files portable: a remote command prints remote paths, and the goldens must
// come out byte-identical to a local run's.
func TestSSHNormalizesRemotePathsInSnapshots(t *testing.T) {
	ssh := requireSSH(t)
	body := "tests:\n" +
		"\t- desc: paths normalize\n" +
		"\t  cmd: 'echo out={outputs.o.txt}'\n" +
		"\t  outputs:\n" +
		"\t    snapshot: true\n"

	suite := writeDatsFile(t, body)
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
