package runner

// The masked-/proc fallback, tested against a REAL kernel refusal rather than against an argv string.

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const maskedProcHelperEnv = "DATS_TEST_MASKED_PROC_CHILD"

func TestBwrapFallsBackWhenTheKernelRefusesAPrivateProcfs(t *testing.T) {
	if os.Getenv(maskedProcHelperEnv) == "1" {
		maskedProcChild(t)
		return
	}
	requireBwrap(t)
	if _, err := exec.LookPath("unshare"); err != nil {
		t.Skip("unshare(1) not available; the masked-/proc case needs it to build the namespace")
	}
	exe, err := os.Executable()
	require.Nil(t, err)
	if err := exec.Command("unshare", append(containerNamespace, "true")...).Run(); err != nil {
		t.Skipf("unprivileged user namespaces unavailable here: %v", err)
	}

	// A marker the child must NOT be able to see.
	marker := exec.Command("sleep", "600")
	require.Nil(t, marker.Start())
	defer func() { _ = marker.Process.Kill() }()

	args := append(append([]string{}, containerNamespace...),
		exe, "-test.run=^"+t.Name()+"$", "-test.v")
	cmd := exec.Command("unshare", args...)
	cmd.Env = append(os.Environ(), maskedProcHelperEnv+"=1",
		outsidePIDEnv+"="+strconv.Itoa(marker.Process.Pid))
	out, err := cmd.CombinedOutput()
	assert.Nil(t, err, "masked-/proc child failed:\n%s", out)
	assert.Contains(t, string(out), "PASS", "child output:\n%s", out)
}

var containerNamespace = []string{"--user", "--map-root-user", "--mount", "--pid", "--fork", "--mount-proc"}

// outsidePIDEnv carries a pid living outside the namespace, which the sandbox must never be able to see.
const outsidePIDEnv = "DATS_TEST_OUTSIDE_PID"

// maskedProcChild runs inside the user+mount namespace.
func maskedProcChild(t *testing.T) {
	path, err := exec.LookPath("bwrap")
	require.Nil(t, err)

	// Obscure /proc the way a container runtime does.
	var masked string
	for _, dir := range []string{"/proc/acpi", "/proc/scsi", "/proc/asound", "/proc/bus", "/proc/irq"} {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		if out, err := exec.Command("mount", "-t", "tmpfs", "-o", "size=0", "tmpfs", dir).CombinedOutput(); err == nil {
			masked = dir
			break
		} else {
			t.Logf("could not mask %s: %v: %s", dir, err, out)
		}
	}
	require.NotEqual(t, "", masked, "could not obscure any /proc subdirectory, so there is nothing to fall back FROM")
	t.Logf("masked %s", masked)

	require.NotNil(t, runBwrapProbe(path, procFresh),
		"a private procfs must be REFUSED once /proc is obscured -- if it succeeded, "+
			"this test is no longer exercising the case it was written for")

	proc, err := probeBwrap()
	require.Nil(t, err, "the probe must still find a usable sandbox, not give up")
	require.Equal(t, procShared, proc, "it must be the read-only-bind shape")

	// And the sandbox it builds has to actually work, and still be a sandbox.
	plan := &sandboxPlan{backend: SandboxBwrap, proc: procShared, network: true, work: t.TempDir()}
	run := func(cmd string) (string, error) {
		argv := plan.bwrapArgv(cmd)
		out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}

	pid, err := run("echo $$")
	require.Nil(t, err, "the fallback sandbox must run commands: %s", pid)
	n, err := strconv.Atoi(pid)
	require.Nil(t, err, "expected a pid, got %q", pid)
	assert.Less(t, n, 10, "the command must still be in its OWN pid namespace (got pid %d): "+
		"the kernel refused a procfs mount, never the namespace, so losing it would be "+
		"trading away containment to buy back concealment", n)

	self, err := run("readlink /proc/self/exe")
	assert.Nil(t, err, "/proc must be usable inside the sandbox: %s", self)
	assert.NotEqual(t, "", self, "/proc/self must resolve, or bash and Go break in ways that look unrelated")

	// THE point of the fallback's gate.
	outside := os.Getenv(outsidePIDEnv)
	require.NotEqual(t, "", outside, "the parent must name a pid living outside this namespace")
	seen, err := run("test -e /proc/" + outside + " && echo VISIBLE || echo hidden")
	assert.Nil(t, err)
	assert.Equal(t, "hidden", seen,
		"a process outside the container is visible from inside the sandbox (pid %s): the "+
			"read-only /proc bind must never reach past the container's own PID namespace", outside)

	// Every pid it CAN see has to be one of the container's own.
	highest, err := run("ls -d /proc/[0-9]* | sed 's@/proc/@@' | sort -n | tail -1")
	assert.Nil(t, err)
	got, err := strconv.Atoi(strings.TrimSpace(highest))
	require.Nil(t, err, "expected a pid, got %q", highest)
	assert.Less(t, got, 1000,
		"the sandbox can see pid %d: a container's PID namespace numbers from 1, so a pid "+
			"this high came from outside it and the bind is reaching past the container", got)

	denied, err := run("touch /usr/dats-pwned 2>&1 || true")
	assert.Nil(t, err)
	assert.Contains(t, denied, "Read-only file system",
		"the host tool tree must stay read-only in the fallback")
}
