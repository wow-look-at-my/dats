package runner

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sshPlan is a plan wired to a target without connecting, which is all the argv builders need.
func sshPlan(t *testing.T, remoteBase string) *sandboxPlan {
	t.Helper()
	return &sandboxPlan{ssh: NewSSHConfig("build@box"), remoteBase: remoteBase, work: t.TempDir()}
}

func TestSSHCommandWrapsTheSameBashInvocation(t *testing.T) {
	plan := sshPlan(t, "/tmp/dats-remote")
	got := plan.command("echo 'hi there' > $X", nil)

	require.NotEmpty(t, got.Argv)
	assert.Equal(t, "ssh", got.Argv[0])
	assert.Equal(t, "build@box", got.Argv[len(got.Argv)-2])
	assert.NotNil(t, got.Kill, "a remote command outlives the client we spawn")

	script := got.Argv[len(got.Argv)-1]
	assert.Contains(t, script, sshQuote("echo 'hi there' > $X"), "the command must travel quoted, verbatim")
	assert.Contains(t, script, "'bash' '-c'", "ssh must wrap the same bash -c every backend ends in")
	assert.Contains(t, script, "exec ", "exec hands the remote exit status back unchanged")
	assert.Contains(t, script, "/tmp/dats-remote/"+sshPidDirName, "the pid must be recorded for the kill hook")
}

func TestSSHCommandForwardsTheAddedEnvironment(t *testing.T) {
	plan := sshPlan(t, "/tmp/dats-remote")
	script := plan.command("true", []string{"MY_VAR=value"}).Argv[len(plan.command("true", nil).Argv)-1]
	assert.Contains(t, script, "env -- ", "an ssh session inherits nothing, so env must be carried")
	assert.Contains(t, script, sshQuote("MY_VAR=value"))
}

func TestSSHPlanDescribesItselfAsUnsandboxed(t *testing.T) {
	desc := sshPlan(t, "/tmp/dats-remote").describe()
	assert.Contains(t, desc, "build@box")
	assert.Contains(t, desc, "none", "a remote run has no sandbox, and the header must not imply one")
}

func TestSSHRefusesATypedSandboxBackend(t *testing.T) {
	for _, mode := range []SandboxMode{SandboxBwrap, SandboxSeatbelt, SandboxDocker} {
		t.Run(string(mode), func(t *testing.T) {
			r := &Runner{ssh: NewSSHConfig("build@box"), Sandbox: NewSandboxConfig(mode, "")}
			_, err := r.newSandboxPlan(nil, t.TempDir())
			require.Error(t, err)
			assert.Contains(t, err.Error(), string(mode))
			assert.Contains(t, err.Error(), "--ssh")
		})
	}
}

func TestSSHUnderAutoSandboxWins(t *testing.T) {
	r := &Runner{ssh: NewSSHConfig("build@box"), Sandbox: NewSandboxConfig(SandboxAuto, "")}
	plan, err := r.newSandboxPlan(nil, t.TempDir())
	require.NoError(t, err)
	require.NotNil(t, plan)
	assert.NotNil(t, plan.ssh, "auto is nobody's explicit choice, so the target takes it")
}

func TestCommandPathRewritesOnlyUnderTheBase(t *testing.T) {
	ctx := &TestContext{BaseDir: "/tmp/dats-local", RemoteBase: "/tmp/dats-remote"}
	assert.Equal(t, "/tmp/dats-remote/test-1/outputs/a.txt",
		ctx.commandPath("/tmp/dats-local/test-1/outputs/a.txt"))
	assert.Equal(t, "/etc/passwd", ctx.commandPath("/etc/passwd"))
	assert.Equal(t, "", ctx.commandPath(""))

	local := &TestContext{BaseDir: "/tmp/dats-local"}
	assert.Equal(t, "/tmp/dats-local/test-1/x", local.commandPath("/tmp/dats-local/test-1/x"),
		"a local run must be byte-identical to before")
}

func TestNormalizeSnapshotTextTokenizesRemotePaths(t *testing.T) {
	ctx := &TestContext{
		BaseDir:    "/tmp/dats-local",
		TestIndex:  2,
		SharedDir:  "/tmp/dats-local/shared",
		RemoteBase: "/tmp/dats-remote",
	}
	got := NormalizeSnapshotText(
		"out=/tmp/dats-remote/test-2/outputs/a.txt cfg=/tmp/dats-remote/shared/c.json root=/tmp/dats-remote", ctx)
	assert.Equal(t, "out={testdir}/outputs/a.txt cfg={shareddir}/c.json root={tmproot}", got)
}

func TestNormalizeSnapshotTextStillTokenizesLocalPaths(t *testing.T) {
	ctx := &TestContext{BaseDir: "/tmp/dats-local", TestIndex: 2, SharedDir: "/tmp/dats-local/shared"}
	got := NormalizeSnapshotText("a=/tmp/dats-local/test-2/x b=/tmp/dats-local/shared/y", ctx)
	assert.Equal(t, "a={testdir}/x b={shareddir}/y", got)
}

func TestExpandPlaceholdersUsesRemotePathsWhenRemote(t *testing.T) {
	ctx := &TestContext{
		BaseDir:    "/tmp/dats-local",
		TestIndex:  0,
		OutputsDir: "/tmp/dats-local/test-0/outputs",
		SharedDir:  "/tmp/dats-local/shared",
		InputPaths: map[string]string{"in.txt": "/tmp/dats-local/test-0/inputs/in.txt"},
		RemoteBase: "/tmp/dats-remote",
	}
	got := ExpandPlaceholders("cat {inputs.in.txt} {shared.c.json} > {outputs.o.txt}", ctx)
	assert.Equal(t, "cat /tmp/dats-remote/test-0/inputs/in.txt /tmp/dats-remote/shared/c.json > /tmp/dats-remote/test-0/outputs/o.txt", got)
}
