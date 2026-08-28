package runner


import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const procGateHelperEnv = "DATS_TEST_PROC_GATE_CHILD"

func TestProcBindGateRefusesTheMachinesOwnPIDNamespace(t *testing.T) {
	if link, err := os.Readlink("/proc/self/ns/pid"); err != nil || link != initialPIDNamespace {
		t.Skipf("these tests already run in a PID namespace (%s); the host case is not reachable here", link)
	}
	why := procBindWouldRevealOutsideProcesses()
	require.NotEqual(t, "", why,
		"on a bare host the read-only /proc bind exposes every process on the machine, "+
			"so the fallback must be refused rather than taken")
	assert.Contains(t, why, "machine's own PID namespace")
}

func TestProcBindGateRefusesAnAncestorProcfs(t *testing.T) {
	if os.Getenv(procGateHelperEnv) == "1" {
		ns, err := os.Readlink("/proc/self/ns/pid")
		require.Nil(t, err)
		require.NotEqual(t, initialPIDNamespace, ns,
			"the harness must put this child in its OWN pid namespace, or it is testing the host case")

		why := procBindWouldRevealOutsideProcesses()
		require.NotEqual(t, "", why,
			"this /proc belongs to an ancestor namespace and lists processes outside this one, "+
				"so the fallback must be refused -- the namespace inode alone does not prove scope")
		assert.Contains(t, why, "ancestor PID namespace")
		return
	}
	if _, err := exec.LookPath("unshare"); err != nil {
		t.Skip("unshare(1) not available; this shape needs it to build the namespace")
	}
	exe, err := os.Executable()
	require.Nil(t, err)
	// Deliberately NO --mount-proc: that omission is the whole test.
	probe := []string{"--user", "--map-root-user", "--pid", "--fork"}
	if err := exec.Command("unshare", append(probe, "true")...).Run(); err != nil {
		t.Skipf("unprivileged user namespaces unavailable here: %v", err)
	}

	cmd := exec.Command("unshare", append(probe, exe, "-test.run=^"+t.Name()+"$", "-test.v")...)
	cmd.Env = append(os.Environ(), procGateHelperEnv+"=1")
	out, err := cmd.CombinedOutput()
	assert.Nil(t, err, "ancestor-procfs child failed:\n%s", out)
	assert.Contains(t, string(out), "PASS", "child output:\n%s", out)
}

func TestNSPIDDepth(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status string
		want   int
	}{
		{"own namespace's procfs", "Name:\tbash\nNSpid:\t3\nThreads:\t1\n", 1},
		{"ancestor's procfs", "Name:\tbash\nNSpid:\t14494\t3\nThreads:\t1\n", 2},
		{"three deep", "NSpid:\t900\t12\t3\n", 3},
		{"field absent", "Name:\tbash\nThreads:\t1\n", 0},
		{"empty", "", 0},
		// NStgid also starts with "NS" and must not be mistaken for it.
		{"NStgid first", "NStgid:\t14494\t3\nNSpid:\t14494\t3\n", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, nspidDepth([]byte(tc.status)))
		})
	}
}
