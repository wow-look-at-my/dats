package runner

// Sandbox tests. The argv builders and backend resolution are tested with
// injected probes (no backend needed); the isolation itself is tested against
// a real bubblewrap, skipped when the host cannot provide one.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wow-look-at-my/dats/schema"
)

// requireBwrap skips a test that needs real isolation when bubblewrap is
// missing or the kernel denies it (containers routinely do).
//
// A skip must never be how CI reports "isolation works", so CI does not rely
// on this: .github/workflows/ci.yml installs bubblewrap, clears the
// ubuntu-24.04 user-namespace restriction, and runs this same probe as its
// own step. An unusable backend fails the build there, before a single test
// runs -- which is what keeps these tests from quietly skipping themselves
// into a green build.
func requireBwrap(t *testing.T) {
	t.Helper()
	if err := probeBwrap(); err != nil {
		t.Skipf("bubblewrap not usable here: %v", err)
	}
}

// sandboxConfigWithProbe builds a config whose backend probes are answered by
// probe instead of the host.
func sandboxConfigWithProbe(mode SandboxMode, probe func(SandboxMode) error) *SandboxConfig {
	cfg := NewSandboxConfig(mode, "")
	cfg.probe = probe
	return cfg
}

func probeAlways(err error) func(SandboxMode) error {
	return func(SandboxMode) error { return err }
}

func TestParseSandboxMode(t *testing.T) {
	for _, name := range []string{"auto", "bwrap", "seatbelt", "docker", "none"} {
		mode, err := ParseSandboxMode(name)
		require.Nil(t, err)
		assert.Equal(t, SandboxMode(name), mode)
	}
	_, err := ParseSandboxMode("firejail")
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "auto, bwrap, seatbelt, docker, or none")
}

func TestSandboxBackendAutoPrefersBwrap(t *testing.T) {
	cfg := sandboxConfigWithProbe(SandboxAuto, probeAlways(nil))
	backend, err := cfg.Backend()
	require.Nil(t, err)
	assert.Equal(t, SandboxBwrap, backend)
}

func TestSandboxBackendAutoPrefersSeatbeltOverDocker(t *testing.T) {
	// On a mac bwrap is simply absent, and the native backend must win over
	// the container fallback -- seatbelt keeps the host's own tools, docker
	// does not.
	cfg := sandboxConfigWithProbe(SandboxAuto, func(mode SandboxMode) error {
		if mode == SandboxBwrap {
			return assertError("bwrap: not found in $PATH")
		}
		return nil
	})
	backend, err := cfg.Backend()
	require.Nil(t, err)
	assert.Equal(t, SandboxSeatbelt, backend)
}

func TestSandboxBackendAutoFallsBackToDocker(t *testing.T) {
	cfg := sandboxConfigWithProbe(SandboxAuto, func(mode SandboxMode) error {
		if mode == SandboxDocker {
			return nil
		}
		return assertError(string(mode) + ": not found in $PATH")
	})
	backend, err := cfg.Backend()
	require.Nil(t, err)
	assert.Equal(t, SandboxDocker, backend)
}

func TestSandboxBackendAutoWithNoBackendErrorsAndNamesTheOptOut(t *testing.T) {
	// The whole point of defaulting to a sandbox is that "no backend" is
	// never resolved by quietly running on the host: it is an error, and the
	// error has to say how to opt out.
	cfg := sandboxConfigWithProbe(SandboxAuto, func(mode SandboxMode) error {
		return assertError(string(mode) + ": unavailable")
	})
	_, err := cfg.Backend()
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "bwrap: unavailable")
	assert.Contains(t, err.Error(), "seatbelt: unavailable")
	assert.Contains(t, err.Error(), "docker: unavailable")
	assert.Contains(t, err.Error(), "--no-sandbox")
	assert.Contains(t, err.Error(), "sandbox: false")
}

func TestSandboxBackendExplicitDoesNotFallBack(t *testing.T) {
	// An operator who asked for bwrap gets bwrap or an error -- never docker,
	// whose isolation and available tooling are entirely different.
	cfg := sandboxConfigWithProbe(SandboxBwrap, func(mode SandboxMode) error {
		if mode == SandboxBwrap {
			return assertError("no user namespaces")
		}
		return nil
	})
	_, err := cfg.Backend()
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "--sandbox=bwrap is not usable here")
	assert.Contains(t, err.Error(), "no user namespaces")
}

func TestSandboxBackendProbesAtMostOnce(t *testing.T) {
	calls := 0
	cfg := sandboxConfigWithProbe(SandboxAuto, func(SandboxMode) error {
		calls++
		return nil
	})
	for range 5 {
		backend, err := cfg.Backend()
		require.Nil(t, err)
		assert.Equal(t, SandboxBwrap, backend)
	}
	assert.Equal(t, 1, calls, "backend detection must be memoized across files and workers")
}

func TestSandboxBackendNoneNeedsNoProbe(t *testing.T) {
	cfg := sandboxConfigWithProbe(SandboxNone, probeAlways(assertError("must not be probed")))
	backend, err := cfg.Backend()
	require.Nil(t, err)
	assert.Equal(t, SandboxNone, backend)
}

func TestBwrapArgvOrderAndBinds(t *testing.T) {
	plan := &sandboxPlan{backend: SandboxBwrap, network: true, work: "/tmp/dats-1", writable: []string{"/var/data"}}
	argv := plan.bwrapArgv("echo hi")

	assert.Equal(t, "bwrap", argv[0])
	assert.Equal(t, []string{"bash", "-c", "echo hi"}, argv[len(argv)-3:])
	joined := strings.Join(argv, " ")
	assert.Contains(t, joined, "--ro-bind / /")
	assert.Contains(t, joined, "--bind /tmp/dats-1 /tmp/dats-1")
	assert.Contains(t, joined, "--bind /var/data /var/data")
	assert.Contains(t, joined, "--die-with-parent")
	assert.NotContains(t, joined, "--unshare-net")
	// Order is load-bearing: the private /tmp must be mounted before the
	// work-directory bind, or a work directory under /tmp (the usual case)
	// would be buried by the tmpfs and every fixture would vanish.
	assert.Less(t, strings.Index(joined, "--tmpfs /tmp"), strings.Index(joined, "--bind /tmp/dats-1"))
	assert.Less(t, strings.Index(joined, "--ro-bind / /"), strings.Index(joined, "--tmpfs /tmp"))
}

func TestBwrapArgvUnshareNetWhenNetworkOff(t *testing.T) {
	plan := &sandboxPlan{backend: SandboxBwrap, network: false, work: "/tmp/dats-1"}
	assert.Contains(t, strings.Join(plan.bwrapArgv("true"), " "), "--unshare-net")
}

func TestDockerArgv(t *testing.T) {
	plan := &sandboxPlan{
		backend: SandboxDocker,
		image:   "debian:stable-slim",
		network: true,
		work:    "/tmp/dats-1",
		workdir: "/home/user/project",
	}
	argv := plan.dockerArgv("dats-7-1", "echo hi", []string{"FOO=bar"})
	joined := strings.Join(argv, " ")

	assert.Equal(t, []string{"docker", "run"}, argv[:2])
	assert.Equal(t, []string{"debian:stable-slim", "bash", "-c", "echo hi"}, argv[len(argv)-4:])
	assert.Contains(t, joined, "--rm")
	assert.Contains(t, joined, "--name dats-7-1")
	assert.Contains(t, joined, "-v /tmp/dats-1:/tmp/dats-1")
	assert.Contains(t, joined, "-v /home/user/project:/home/user/project:ro")
	assert.Contains(t, joined, "-w /home/user/project")
	assert.Contains(t, joined, "-e FOO=bar")
	assert.NotContains(t, joined, "--network none")
}

func TestDockerArgvNetworkOff(t *testing.T) {
	plan := &sandboxPlan{backend: SandboxDocker, image: "img", network: false, work: "/tmp/w"}
	assert.Contains(t, strings.Join(plan.dockerArgv("n", "true", nil), " "), "--network none")
}

func TestDockerArgvWritableWorkdirIsNotRemountedReadOnly(t *testing.T) {
	// Mounting the same target twice makes docker refuse to start, and
	// demoting an explicitly writable path to the read-only working-directory
	// mount would silently break the very thing it was declared for.
	plan := &sandboxPlan{
		backend:  SandboxDocker,
		image:    "img",
		network:  true,
		work:     "/tmp/dats-1",
		writable: []string{"/home/user/project"},
		workdir:  "/home/user/project",
	}
	joined := strings.Join(plan.dockerArgv("n", "true", nil), " ")
	assert.Contains(t, joined, "-v /home/user/project:/home/user/project ")
	assert.NotContains(t, joined, ":/home/user/project:ro")
	assert.Contains(t, joined, "-w /home/user/project")
}

func TestNewSandboxPlanDisabledPaths(t *testing.T) {
	enabled, disabled := true, false

	// No config at all: the library default, commands run on the host.
	r := &Runner{}
	plan, err := r.newSandboxPlan(nil, "/tmp/w")
	require.Nil(t, err)
	assert.Nil(t, plan)

	// --sandbox=none wins over a file that asks to be sandboxed: the
	// operator's choice is the outer bound, a file can only narrow it.
	r = &Runner{Sandbox: sandboxConfigWithProbe(SandboxNone, probeAlways(nil))}
	plan, err = r.newSandboxPlan(&schema.SandboxSpec{Enabled: &enabled}, "/tmp/w")
	require.Nil(t, err)
	assert.Nil(t, plan)

	// The file-level opt-out, with a backend available.
	r = &Runner{Sandbox: sandboxConfigWithProbe(SandboxAuto, probeAlways(nil))}
	plan, err = r.newSandboxPlan(&schema.SandboxSpec{Enabled: &disabled}, "/tmp/w")
	require.Nil(t, err)
	assert.Nil(t, plan)
}

func TestNewSandboxPlanOptOutNeedsNoBackend(t *testing.T) {
	// A file that opts out must run on a machine with no backend installed:
	// resolution is lazy precisely so `sandbox: false` costs nothing.
	disabled := false
	probed := false
	r := &Runner{Sandbox: sandboxConfigWithProbe(SandboxAuto, func(SandboxMode) error {
		probed = true
		return assertError("unavailable")
	})}
	plan, err := r.newSandboxPlan(&schema.SandboxSpec{Enabled: &disabled}, "/tmp/w")
	require.Nil(t, err)
	assert.Nil(t, plan)
	assert.False(t, probed, "an opted-out file must not probe for a backend")
}

func TestNewSandboxPlanFields(t *testing.T) {
	network := false
	r := &Runner{
		Sandbox:  sandboxConfigWithProbe(SandboxDocker, probeAlways(nil)),
		CoverDir: t.TempDir(),
	}
	r.Sandbox.Image = "custom:tag"
	plan, err := r.newSandboxPlan(&schema.SandboxSpec{
		Network:  &network,
		Image:    "file:tag",
		Writable: []string{"/var/data"},
	}, "/tmp/dats-2")
	require.Nil(t, err)
	require.NotNil(t, plan)

	assert.Equal(t, SandboxDocker, plan.backend)
	assert.Equal(t, "file:tag", plan.image, "the file's image overrides the CLI's")
	assert.False(t, plan.network)
	assert.Equal(t, "/tmp/dats-2", plan.work)
	assert.Contains(t, plan.writable, "/var/data")
	// Coverage data is written by the sandboxed process into a host directory
	// outside the temp tree, so it has to be writable too.
	assert.Contains(t, plan.writable, r.CoverDir)
	assert.Equal(t, "docker file:tag (no network)", plan.describe())
}

func TestSandboxPlanDescribe(t *testing.T) {
	assert.Equal(t, "", (*sandboxPlan)(nil).describe(), "an unsandboxed run announces nothing")
	assert.Equal(t, "bwrap", (&sandboxPlan{backend: SandboxBwrap, network: true}).describe())
}

// assertError is a tiny error constructor for the injected probes.
type assertError string

func (e assertError) Error() string { return string(e) }

func TestSeatbeltArgv(t *testing.T) {
	plan := &sandboxPlan{backend: SandboxSeatbelt, network: true, work: "/tmp/dats-1"}
	argv := plan.seatbeltArgv("echo hi")

	require.Len(t, argv, 6)
	assert.Equal(t, "sandbox-exec", argv[0])
	assert.Equal(t, "-p", argv[1], "the profile is passed inline, not through a temp file")
	assert.Contains(t, argv[2], "(version 1)")
	assert.Contains(t, argv[2], `(subpath "/tmp/dats-1")`)
	assert.Equal(t, []string{"bash", "-c", "echo hi"}, argv[3:])
}

func TestSeatbeltProfileShape(t *testing.T) {
	profile := seatbeltProfile([]string{"/tmp/dats-1", "/var/data"}, true)

	assert.Contains(t, profile, "(version 1)")
	assert.Contains(t, profile, `(subpath "/tmp/dats-1")`)
	assert.Contains(t, profile, `(subpath "/var/data")`)
	assert.Contains(t, profile, `(literal "/dev/null")`, "a shell that cannot write /dev/null is not a usable shell")
	assert.NotContains(t, profile, "(deny network*)", "the network stays on unless a file turns it off")

	// SBPL is last-match-wins, so this order IS the policy: allow everything,
	// deny all writes, then re-allow the file's own directories. Any other
	// order either denies everything or allows every write.
	allowAll := strings.Index(profile, "(allow default)")
	denyWrites := strings.Index(profile, "(deny file-write*)")
	allowWrites := strings.Index(profile, "(allow file-write*")
	assert.Less(t, allowAll, denyWrites)
	assert.Less(t, denyWrites, allowWrites)
}

func TestSeatbeltProfileNetworkOff(t *testing.T) {
	profile := seatbeltProfile([]string{"/tmp/dats-1"}, false)
	assert.Contains(t, profile, "(deny network*)")
	// The deny must not land after the write rules, where it would be fine,
	// nor before (allow default), where it would be overridden.
	assert.Less(t, strings.Index(profile, "(allow default)"), strings.Index(profile, "(deny network*)"))
}

func TestSeatbeltProfileEscapesPaths(t *testing.T) {
	// A quote in a declared writable path would otherwise end the literal
	// early and change which paths the profile allows.
	profile := seatbeltProfile([]string{`/tmp/we"ird\path`}, true)
	assert.Contains(t, profile, `(subpath "/tmp/we\"ird\\path")`)
}

func TestSeatbeltWritablePathsResolveSymlinks(t *testing.T) {
	// The macOS trap this exists for: the sandbox matches the REAL path, but
	// dats' own temp dirs routinely arrive through symlinks (/tmp ->
	// /private/tmp). An unresolved subpath rule matches nothing and every
	// fixture write is denied.
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	require.Nil(t, os.Symlink(real, link))

	plan := &sandboxPlan{backend: SandboxSeatbelt, network: true, work: link}
	paths := plan.seatbeltWritablePaths()

	realResolved, err := filepath.EvalSymlinks(real)
	require.Nil(t, err)
	assert.Contains(t, paths, realResolved, "the resolved path is what the sandbox matches")
	assert.Contains(t, paths, link, "the unresolved form is kept too: commands may address either")
}

func TestSeatbeltWritablePathsKeepsUnresolvablePaths(t *testing.T) {
	// A path that does not exist yet must not vanish from the profile: a
	// dropped rule silently widens nothing but denies the write it was
	// declared for, and a missing rule is far harder to diagnose than a
	// denial.
	plan := &sandboxPlan{backend: SandboxSeatbelt, network: true, work: "/definitely/not/here"}
	assert.Contains(t, plan.seatbeltWritablePaths(), "/definitely/not/here")
}

func TestSandboxPlanDescribeSeatbelt(t *testing.T) {
	assert.Equal(t, "seatbelt", (&sandboxPlan{backend: SandboxSeatbelt, network: true}).describe())
	assert.Equal(t, "seatbelt (no network)", (&sandboxPlan{backend: SandboxSeatbelt}).describe())
}
