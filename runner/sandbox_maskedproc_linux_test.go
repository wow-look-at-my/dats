package runner

// The masked-/proc fallback, tested against a REAL kernel refusal rather than
// against an argv string. A container runtime obscures parts of /proc, and the
// kernel then refuses the sandbox a private procfs -- which is the whole
// reason procShared exists, and is not something an argv assertion can show.
//
// Nothing here needs privilege or a container: one tmpfs over one /proc
// subdirectory inside a user namespace reproduces it exactly, because the rule
// is about ANY locked mount obscuring the procfs, not about docker's mask set.
// The test re-execs itself under `unshare` because a Go process is
// multithreaded by the time a test runs, and unshare(CLONE_NEWUSER) is refused
// to a multithreaded process.

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// maskedProcHelperEnv marks the re-exec'd child, which is the same test binary
// running the same test from inside the namespace.
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
	// A bare namespace first: where the kernel denies unprivileged user
	// namespaces there is nothing to test, and a failure here would report
	// that as a fallback bug.
	if err := exec.Command("unshare", "--user", "--map-root-user", "--mount", "true").Run(); err != nil {
		t.Skipf("unprivileged user namespaces unavailable here: %v", err)
	}

	cmd := exec.Command("unshare", "--user", "--map-root-user", "--mount",
		exe, "-test.run=^"+t.Name()+"$", "-test.v")
	cmd.Env = append(os.Environ(), maskedProcHelperEnv+"=1")
	out, err := cmd.CombinedOutput()
	assert.Nil(t, err, "masked-/proc child failed:\n%s", out)
	assert.Contains(t, string(out), "PASS", "child output:\n%s", out)
}

// maskedProcChild runs inside the user+mount namespace.
func maskedProcChild(t *testing.T) {
	path, err := exec.LookPath("bwrap")
	require.Nil(t, err)

	// Obscure /proc the way a container runtime does. Any one subdirectory is
	// enough; docker covers several, and which ones exist varies by kernel.
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

	// The negative control, and the reason this test exists: the shape dats
	// asks for first must genuinely be refused here. Without this the test
	// would pass just as happily on a host that never needed a fallback.
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

	denied, err := run("touch /usr/dats-pwned 2>&1 || true")
	assert.Nil(t, err)
	assert.Contains(t, denied, "Read-only file system",
		"the host tool tree must stay read-only in the fallback")
}
