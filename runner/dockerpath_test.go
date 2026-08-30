package runner

import (
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// forceDaemonOS answers the daemon probe with what the named daemon reports,
// and clears the memo so each case resolves the mapper again.
func forceDaemonOS(t *testing.T, reported string) {
	t.Helper()
	previousFn, previousProbe := daemonPathFn, dockerInfoOS
	daemonPathOnce = sync.Once{}
	daemonPathFn = nil
	dockerInfoOS = func() string { return reported }
	t.Cleanup(func() {
		daemonPathOnce = sync.Once{}
		daemonPathFn, dockerInfoOS = previousFn, previousProbe
	})
}

func TestDaemonPathLeavesAPosixHostAlone(t *testing.T) {
	forceHostGOOS(t, "linux")
	forceDaemonOS(t, "Ubuntu 24.04.4 LTS")
	assert.Equal(t, "/tmp/dats-x/test-1", daemonPath("/tmp/dats-x/test-1"))
}

func TestDaemonPathLeavesDockerDesktopAlone(t *testing.T) {
	forceHostGOOS(t, "windows")
	forceDaemonOS(t, "Docker Desktop")
	assert.Equal(t, `D:\a\x`, daemonPath(`D:\a\x`))
}

func TestDaemonPathPutsAnNTDriveUnderMntForALinuxDaemon(t *testing.T) {
	forceHostGOOS(t, "windows")
	forceDaemonOS(t, "Ubuntu 24.04.4 LTS")
	assert.Equal(t, "/mnt/d/a/go-toolchain", daemonPath(`D:\a\go-toolchain`))
	assert.Equal(t, "/mnt/c/Users/RUNNER/Temp/dats-x", daemonPath("C:/Users/RUNNER/Temp/dats-x"))
}

func TestDockerArgvBindsWhereALinuxDaemonCanSeeIt(t *testing.T) {
	forceHostGOOS(t, "windows")
	forceDaemonOS(t, "Ubuntu 24.04.4 LTS")
	plan := &sandboxPlan{
		backend: SandboxDocker,
		image:   "golang:1.25",
		network: true,
		work:    `C:\t\dats-x`,
		workdir: `D:\a\go-toolchain\go-toolchain`,
	}

	argv := plan.dockerArgv("dats-probe", "cat C:/t/dats-x/test-1/inputs/a.txt", nil)
	joined := strings.Join(argv, " ")

	assert.Contains(t, joined, "-v /mnt/c/t/dats-x:/mnt/c/t/dats-x")
	assert.Contains(t, joined, "-v /mnt/d/a/go-toolchain/go-toolchain:/mnt/d/a/go-toolchain/go-toolchain:ro")
	assert.Contains(t, joined, "-w /mnt/d/a/go-toolchain/go-toolchain")
	require.NotEmpty(t, argv)
	assert.Equal(t, "cat /mnt/c/t/dats-x/test-1/inputs/a.txt", argv[len(argv)-1],
		"a bind the daemon cannot resolve mounts an EMPTY directory, so the command has to name the mounted spelling")
}

func TestDockerArgvKeepsPosixPathsAsTheyAre(t *testing.T) {
	forceHostGOOS(t, "linux")
	forceDaemonOS(t, "Ubuntu 24.04.4 LTS")
	plan := &sandboxPlan{
		backend: SandboxDocker,
		image:   "golang:1.25",
		network: true,
		work:    "/tmp/dats-x",
		workdir: "/home/user/repo",
	}

	joined := strings.Join(plan.dockerArgv("dats-probe", "cat /tmp/dats-x/test-1/inputs/a.txt", nil), " ")

	assert.Contains(t, joined, "-v /tmp/dats-x:/tmp/dats-x")
	assert.Contains(t, joined, "-w /home/user/repo")
	assert.Contains(t, joined, "cat /tmp/dats-x/test-1/inputs/a.txt")
}
