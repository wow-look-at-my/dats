package runner

// Sandbox tests. The argv builders and backend resolution are tested with
// injected probes (no backend needed); the isolation itself is tested against
// a real bubblewrap, skipped when the host cannot provide one.

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wow-look-at-my/dats/schema"
	"github.com/wow-look-at-my/go-containers/set"
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
	if _, err := probeBwrap(); err != nil {
		t.Skipf("bubblewrap not usable here: %v", err)
	}
}

// sandboxConfigWithProbe builds a config whose backend probes are answered by
// probe instead of the host.
func sandboxConfigWithProbe(mode SandboxMode, probe func(SandboxMode) (procMode, error)) *SandboxConfig {
	cfg := NewSandboxConfig(mode, "")
	cfg.probe = probe
	return cfg
}

func probeAlways(err error) func(SandboxMode) (procMode, error) {
	return func(SandboxMode) (procMode, error) { return procFresh, err }
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
	cfg := sandboxConfigWithProbe(SandboxAuto, func(mode SandboxMode) (procMode, error) {
		if mode == SandboxBwrap {
			return procFresh, assertError("bwrap: not found in $PATH")
		}
		return procFresh, nil
	})
	backend, err := cfg.Backend()
	require.Nil(t, err)
	assert.Equal(t, SandboxSeatbelt, backend)
}

func TestSandboxBackendAutoFallsBackToDocker(t *testing.T) {
	cfg := sandboxConfigWithProbe(SandboxAuto, func(mode SandboxMode) (procMode, error) {
		if mode == SandboxDocker {
			return procFresh, nil
		}
		return procFresh, assertError(string(mode) + ": not found in $PATH")
	})
	backend, err := cfg.Backend()
	require.Nil(t, err)
	assert.Equal(t, SandboxDocker, backend)
}

func TestSandboxBackendAutoWithNoBackendErrorsAndNamesTheOptOut(t *testing.T) {
	// The whole point of defaulting to a sandbox is that "no backend" is
	// never resolved by quietly running on the host: it is an error, and the
	// error has to say how to opt out.
	cfg := sandboxConfigWithProbe(SandboxAuto, func(mode SandboxMode) (procMode, error) {
		return procFresh, assertError(string(mode) + ": unavailable")
	})
	_, err := cfg.Backend()
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "bwrap: unavailable")
	assert.Contains(t, err.Error(), "seatbelt: unavailable")
	assert.Contains(t, err.Error(), "docker: unavailable")
	assert.Contains(t, err.Error(), "--no-sandbox")
}

func TestSandboxBackendExplicitDoesNotFallBack(t *testing.T) {
	// An operator who asked for bwrap gets bwrap or an error -- never docker,
	// whose isolation and available tooling are entirely different.
	cfg := sandboxConfigWithProbe(SandboxBwrap, func(mode SandboxMode) (procMode, error) {
		if mode == SandboxBwrap {
			return procFresh, assertError("no user namespaces")
		}
		return procFresh, nil
	})
	_, err := cfg.Backend()
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "--sandbox=bwrap is not usable here")
	assert.Contains(t, err.Error(), "no user namespaces")
}

func TestSandboxBackendProbesAtMostOnce(t *testing.T) {
	calls := 0
	cfg := sandboxConfigWithProbe(SandboxAuto, func(SandboxMode) (procMode, error) {
		calls++
		return procFresh, nil
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
	plan := &sandboxPlan{
		backend:  SandboxBwrap,
		network:  true,
		work:     "/tmp/dats-1",
		coverDir: "/srv/coverage",
		workdir:  "/home/user/project",
	}
	argv := plan.bwrapArgv("echo hi")

	assert.Equal(t, "bwrap", argv[0])
	assert.Equal(t, []string{"bash", "-c", "echo hi"}, argv[len(argv)-3:])
	joined := strings.Join(argv, " ")
	assert.Contains(t, joined, "--ro-bind-try /usr /usr")
	assert.Contains(t, joined, "--ro-bind-try /home/user/project /home/user/project")
	assert.Contains(t, joined, "--bind /tmp/dats-1 /tmp/dats-1")
	assert.Contains(t, joined, "--bind /srv/coverage /srv/coverage")
	assert.Contains(t, joined, "--chdir /home/user/project")
	assert.Equal(t, 1, strings.Count(joined, "--chdir"),
		"more than one --chdir makes bwrap warn on stderr, into the command's captured output")
	assert.Contains(t, joined, "--die-with-parent")
	assert.NotContains(t, joined, "--unshare-net")
	// Order is load-bearing: the private /tmp must be mounted before the
	// work-directory bind, or a work directory under /tmp (the usual case)
	// would be buried by the tmpfs and every fixture would vanish.
	assert.Less(t, strings.Index(joined, "--tmpfs /tmp"), strings.Index(joined, "--bind /tmp/dats-1"))
	assert.Less(t, strings.Index(joined, "--ro-bind-try /usr /usr"), strings.Index(joined, "--tmpfs /tmp"))
	// The working directory is bound before the writable paths, so a writable
	// path inside it wins -- the precedence docker's mount dedup gives it too.
	assert.Less(t, strings.Index(joined, "--ro-bind-try /home/user/project"), strings.Index(joined, "--bind /srv/coverage"))
	// ...and the --chdir into it comes after the bind that creates it.
	assert.Less(t, strings.Index(joined, "--ro-bind-try /home/user/project"), strings.Index(joined, "--chdir /home/user/project"))
}

// TestBwrapArgvNeverBindsTheHostRoot is the regression test for the bypass
// this backend used to have: `--ro-bind / /` handed every command the whole
// host -- $HOME and its credentials, /var, every other checkout on the machine
// -- and made bwrap expose a completely different filesystem than docker.
func TestBwrapArgvNeverBindsTheHostRoot(t *testing.T) {
	plan := &sandboxPlan{backend: SandboxBwrap, network: true, work: "/tmp/dats-1", workdir: "/home/user/project"}
	argv := plan.bwrapArgv("true")

	assert.NotContains(t, strings.Join(argv, " "), "--ro-bind / /")
	for i, arg := range argv {
		switch arg {
		case "--ro-bind", "--ro-bind-try", "--bind":
			require.Less(t, i+1, len(argv))
			src := argv[i+1]
			assert.NotEqual(t, "/", src, "the host root must never be bound")
			resolvConf, _ := resolvConfTarget()
			if underToolTree(src) || src == plan.work || src == plan.workdir || src == resolvConf {
				continue
			}
			// The only other allowance is the resolv.conf target on
			// systemd-resolved hosts: one FILE, not a host data tree.
			assert.False(t, strings.HasPrefix(src, "/home/") || strings.HasPrefix(src, "/root"),
				"bwrap bound host data path %q", src)
		}
	}
}

// TestToolTreeCoversAddOnToolchains pins /opt in the tool tree. A workflow
// that runs actions/setup-go (or -node, or -python) puts the toolchain under
// /opt/hostedtoolcache and puts it on PATH; if the sandbox does not expose it,
// every sandboxed command loses the interpreter the workflow just installed,
// and the failure surfaces far from anything mentioning a sandbox -- as a tool
// "not found in PATH" inside a suite that passes locally.
func TestToolTreeCoversAddOnToolchains(t *testing.T) {
	assert.Contains(t, toolTreePaths, "/opt")

	plan := &sandboxPlan{backend: SandboxBwrap, network: true, work: "/tmp/dats-1", workdir: "/home/user/project"}
	assert.Contains(t, strings.Join(plan.bwrapArgv("true"), " "), "--ro-bind-try /opt /opt")

	// It stays a tool tree, not a second host: read-only, and never a bind of
	// anything holding user data.
	assert.True(t, underToolTree("/opt/hostedtoolcache/go/1.25.0/x64/bin/go"))
	assert.False(t, underToolTree("/home/runner/work"))
}

// TestBwrapAndDockerExposeTheSameHostPaths pins the property the backends must
// share: of the HOST, a command sees the working directory read-only and the
// file's temp directory read-write, and nothing else. (System tools differ by
// construction -- the image under docker, the tool tree under bwrap.)
func TestBwrapAndDockerExposeTheSameHostPaths(t *testing.T) {
	plan := &sandboxPlan{
		backend:  SandboxBwrap,
		image:    "img",
		network:  true,
		work:     "/tmp/dats-1",
		coverDir: "/srv/coverage",
		workdir:  "/home/user/project",
	}

	// The resolver's config file is the documented single exception: bwrap
	// binds it so a sandboxed command has DNS at all, where docker gets it
	// from the container runtime.
	resolvConf, _ := resolvConfTarget()

	bwrapRO, bwrapRW := map[string]bool{}, map[string]bool{}
	argv := plan.bwrapArgv("true")
	for i, arg := range argv {
		if i+1 >= len(argv) || underToolTree(argv[i+1]) || argv[i+1] == resolvConf {
			continue
		}
		switch arg {
		case "--ro-bind", "--ro-bind-try":
			bwrapRO[argv[i+1]] = true
		case "--bind":
			bwrapRW[argv[i+1]] = true
		}
	}

	dockerRO, dockerRW := map[string]bool{}, map[string]bool{}
	dargv := plan.dockerArgv("n", "true", nil)
	for i, arg := range dargv {
		if arg != "-v" || i+1 >= len(dargv) {
			continue
		}
		src, rest, _ := strings.Cut(dargv[i+1], ":")
		if strings.HasSuffix(rest, ":ro") {
			dockerRO[src] = true
		} else {
			dockerRW[src] = true
		}
	}

	assert.Equal(t, dockerRO, bwrapRO, "read-only host paths must match between backends")
	assert.Equal(t, dockerRW, bwrapRW, "read-write host paths must match between backends")
}

func TestInheritedEnvCarriesCallerVarsButNotImageOwnedOnes(t *testing.T) {
	t.Setenv("DATS_TEST_CALLER_VAR", "carried")
	t.Setenv("HOME", "/host/home")

	env := inheritedEnv()
	assert.Contains(t, env, "DATS_TEST_CALLER_VAR=carried")
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		assert.NotContains(t, []string{"PATH", "HOME", "TMPDIR", "PWD"}, name,
			"%s belongs to the image, not the caller", name)
	}
}

func TestDockerArgvForwardsTheRunEnvironment(t *testing.T) {
	// The bwrap child inherits the run's environment; the container must be
	// handed the same variables, or one suite reads them under one backend and
	// reads empty strings under the other.
	t.Setenv("DATS_TEST_HANDOFF_DIR", "/home/user/project/build/.stage")
	plan := &sandboxPlan{backend: SandboxDocker, image: "img", network: true, work: "/tmp/dats-1"}
	joined := strings.Join(plan.dockerArgv("n", "true", []string{"FOO=bar"}), " ")

	assert.Contains(t, joined, "-e DATS_TEST_HANDOFF_DIR=/home/user/project/build/.stage")
	assert.Contains(t, joined, "-e FOO=bar")
	// dats' own additions come last, so a test's inputs.env wins over an
	// inherited variable of the same name.
	assert.Less(t, strings.Index(joined, "-e DATS_TEST_HANDOFF_DIR"), strings.Index(joined, "-e FOO=bar"))
}

// The fallback /proc shape swaps ONE argument and keeps everything that
// contains the command. A masked container refuses a private procfs
// (mount_too_revealing), and the wrong response would be to drop the PID
// namespace with it: that is the argument doing the containing, and the kernel
// never objected to it.
func TestBwrapSharedProcKeepsTheContainment(t *testing.T) {
	fresh := bwrapIsolationArgs(procFresh)
	shared := bwrapIsolationArgs(procShared)

	assert.Contains(t, fresh, "--proc", "the private procfs is what dats asks for first")
	assert.NotContains(t, shared, "--proc",
		"the fallback exists because the kernel refuses --proc here; asking again just fails again")

	i := slices.Index(shared, "--ro-bind")
	require.NotEqual(t, -1, i, "the fallback must bind the existing /proc")
	assert.Equal(t, []string{"--ro-bind", "/proc", "/proc"}, shared[i:i+3],
		"the bind is READ-ONLY: it is what keeps /proc/sysrq-trigger and /proc/sys "+
			"unwritable inside the sandbox")

	for _, keep := range []string{"--unshare-user", "--unshare-pid", "--die-with-parent", "--tmpfs"} {
		assert.Contains(t, shared, keep,
			"%s must survive the fallback: the refusal is about mounting a procfs, "+
				"never about the isolation, so nothing else may be traded away", keep)
	}

	// Everything except the /proc argument pair is identical, so the fallback
	// cannot quietly widen anything else.
	assert.Equal(t, drop(fresh, "--proc", "/proc"), drop(shared, "--ro-bind", "/proc", "/proc"),
		"the two shapes must differ ONLY in how /proc is provided")
}

// drop returns argv without the first run of the given consecutive arguments.
func drop(argv []string, run ...string) []string {
	for i := range argv {
		if i+len(run) <= len(argv) && slices.Equal(argv[i:i+len(run)], run) {
			return slices.Concat(argv[:i:i], argv[i+len(run):])
		}
	}
	return argv
}

// The probe asks for the strong sandbox first and only settles for the weak
// one when the kernel refuses -- never the other way round, or a host that
// could isolate properly would silently stop doing so.
func TestProbeBwrapPrefersThePrivateProcfs(t *testing.T) {
	requireBwrap(t)
	path, err := exec.LookPath("bwrap")
	require.Nil(t, err)

	// Whatever this host can do, the fresh shape's own result decides: the
	// probe must never report procShared where procFresh works.
	proc, err := probeBwrap()
	require.Nil(t, err)
	if runBwrapProbe(path, procFresh) == nil {
		assert.Equal(t, procFresh, proc,
			"a host that can mount a private procfs must get one")
	} else {
		assert.Equal(t, procShared, proc,
			"a host that cannot must still get a sandbox, not an error")
	}
}

// A reduced sandbox is announced on every file it applies to, and explained
// once. Silence here would be the whole point missed: the run would look
// exactly like one that got the isolation it asked for.
func TestSharedProcIsAnnouncedAndExplainedOnce(t *testing.T) {
	assert.Equal(t, "bwrap", (&sandboxPlan{backend: SandboxBwrap, network: true}).describe(),
		"the strong sandbox says nothing extra")
	assert.Equal(t, "bwrap (shared /proc)",
		(&sandboxPlan{backend: SandboxBwrap, proc: procShared, network: true}).describe())

	cfg := NewSandboxConfig(SandboxBwrap, "")
	cfg.backend, cfg.proc = SandboxBwrap, procShared
	first := cfg.TakeProcNotice()
	assert.Contains(t, first, "CAN see this container's process list",
		"the explanation must state what was lost, not just that something was")
	assert.Equal(t, "", cfg.TakeProcNotice(), "explained once per run, not once per file")

	strong := NewSandboxConfig(SandboxBwrap, "")
	strong.backend = SandboxBwrap
	assert.Equal(t, "", strong.TakeProcNotice(), "nothing to explain when nothing was lost")
	assert.Equal(t, "", (*SandboxConfig)(nil).TakeProcNotice())
}

// The sandbox must ask for a USER namespace, and ask for it before the
// namespaces nested inside it.
//
// Creating a mount or PID namespace directly requires CAP_SYS_ADMIN; creating
// one inside a user namespace does not. An unprivileged container holds no
// such capability, so without --unshare-user bwrap is refused at the PID
// namespace and reports "Creating new namespace failed: Operation not
// permitted" -- an error naming no namespace, which is why this went
// undiagnosed while every argv test here passed.
func TestBwrapArgvUnsharesTheUserNamespaceFirst(t *testing.T) {
	argv := bwrapIsolationArgs(procFresh)

	user := slices.Index(argv, "--unshare-user")
	require.NotEqual(t, -1, user,
		"bwrap must unshare the user namespace: without it the pid and mount "+
			"namespaces need CAP_SYS_ADMIN, which an unprivileged container lacks")

	pid := slices.Index(argv, "--unshare-pid")
	require.NotEqual(t, -1, pid)
	assert.Less(t, user, pid,
		"--unshare-user must come before --unshare-pid: the pid namespace is created "+
			"inside the user namespace, which is what removes the capability requirement")

	// And BOTH must precede the filesystem setup. bwrap applies arguments in
	// order, so a --proc read before --unshare-pid mounts the HOST's procfs --
	// which an unprivileged container is refused, with an error that names the
	// mount rather than the ordering: "Can't mount proc on /newroot/proc:
	// Operation not permitted". The flags were all present when that happened;
	// only their order was wrong, which is why this asserts position and not
	// membership.
	for _, later := range []string{"--proc", "--dev", "--tmpfs"} {
		i := slices.Index(argv, later)
		require.NotEqual(t, -1, i, "%s must be in the isolation args", later)
		assert.Less(t, pid, i,
			"%s must come AFTER --unshare-pid: bwrap applies arguments in order, so a "+
				"filesystem set up before the namespaces exist is set up in the HOST's, "+
				"which an unprivileged container cannot do", later)
	}
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
		coverDir: "/home/user/project",
		workdir:  "/home/user/project",
	}
	joined := strings.Join(plan.dockerArgv("n", "true", nil), " ")
	assert.Contains(t, joined, "-v /home/user/project:/home/user/project ")
	assert.NotContains(t, joined, ":/home/user/project:ro")
	assert.Contains(t, joined, "-w /home/user/project")
}

func TestNewSandboxPlanDisabledPaths(t *testing.T) {
	network := false

	// No config at all: the library default, commands run on the host.
	r := &Runner{}
	plan, err := r.newSandboxPlan(nil, "/tmp/w")
	require.Nil(t, err)
	assert.Nil(t, plan)

	// --sandbox=none is the ONLY way a plan comes out nil with a file that
	// narrowed its sandbox: the operator's choice is the outer bound, and a
	// file's own block can only tighten what it is handed.
	r = &Runner{Sandbox: sandboxConfigWithProbe(SandboxNone, probeAlways(nil))}
	plan, err = r.newSandboxPlan(&schema.SandboxSpec{Network: &network}, "/tmp/w")
	require.Nil(t, err)
	assert.Nil(t, plan)
}

func TestNewSandboxPlanOptOutNeedsNoBackend(t *testing.T) {
	// An opted-out run must work on a machine with no backend installed:
	// resolution is lazy precisely so --no-sandbox costs nothing.
	probed := false
	r := &Runner{Sandbox: sandboxConfigWithProbe(SandboxNone, func(SandboxMode) (procMode, error) {
		probed = true
		return procFresh, assertError("unavailable")
	})}
	plan, err := r.newSandboxPlan(nil, "/tmp/w")
	require.Nil(t, err)
	assert.Nil(t, plan)
	assert.False(t, probed, "an opted-out run must not probe for a backend")
}

// TestNewSandboxPlanFileNarrowingStillSandboxes pins the direction of travel:
// a file that states anything about its sandbox still gets one.
func TestNewSandboxPlanFileNarrowingStillSandboxes(t *testing.T) {
	network := false
	r := &Runner{Sandbox: sandboxConfigWithProbe(SandboxAuto, probeAlways(nil))}
	plan, err := r.newSandboxPlan(&schema.SandboxSpec{Network: &network}, "/tmp/w")
	require.Nil(t, err)
	require.NotNil(t, plan)
	assert.False(t, plan.network)
}

func TestNewSandboxPlanFields(t *testing.T) {
	network := false
	r := &Runner{
		Sandbox:  sandboxConfigWithProbe(SandboxDocker, probeAlways(nil)),
		CoverDir: t.TempDir(),
	}
	plan, err := r.newSandboxPlan(&schema.SandboxSpec{
		Network: &network,
		Image:   "file:tag",
	}, "/tmp/dats-2")
	require.Nil(t, err)
	require.NotNil(t, plan)

	assert.Equal(t, SandboxDocker, plan.backend)
	assert.Equal(t, "file:tag", plan.image, "with no image from the operator, the file picks")
	assert.False(t, plan.network)
	assert.Equal(t, "/tmp/dats-2", plan.work)
	// Coverage data is written by the sandboxed process into a host directory
	// outside the temp tree, so it has to be writable too.
	assert.Contains(t, plan.writablePaths(), r.CoverDir)
	assert.Equal(t, "docker file:tag (no network)", plan.describe())
}

// TestNewSandboxPlanImagePrecedence: an image the operator typed is a decision
// about what gets pulled and run on their machine, so a file cannot swap it
// out -- the same rule as the sandbox itself, one level down. When both name
// one, the run SAYS the file's was refused; a suite quietly running in an
// image it did not ask for fails later, somewhere that never mentions images.
func TestNewSandboxPlanImagePrecedence(t *testing.T) {
	newRunner := func(operatorImage string) *Runner {
		r := &Runner{Sandbox: sandboxConfigWithProbe(SandboxDocker, probeAlways(nil))}
		r.Sandbox.Image = operatorImage
		return r
	}

	// Operator pinned one, file wants another: the operator's wins, out loud.
	plan, err := newRunner("pinned:tag").newSandboxPlan(&schema.SandboxSpec{Image: "file:tag"}, "/tmp/w")
	require.Nil(t, err)
	assert.Equal(t, "pinned:tag", plan.image)
	assert.Equal(t, "file:tag", plan.refusedImage)
	assert.Equal(t, "docker pinned:tag (--sandbox-image; file asked for file:tag)", plan.describe())

	// Agreeing on the same image is not a refusal to announce.
	plan, err = newRunner("same:tag").newSandboxPlan(&schema.SandboxSpec{Image: "same:tag"}, "/tmp/w")
	require.Nil(t, err)
	assert.Equal(t, "same:tag", plan.image)
	assert.Equal(t, "", plan.refusedImage)
	assert.Equal(t, "docker same:tag", plan.describe())

	// Neither named one: the default, and nothing to announce.
	plan, err = newRunner("").newSandboxPlan(nil, "/tmp/w")
	require.Nil(t, err)
	assert.Equal(t, DefaultSandboxImage, plan.image)
	assert.Equal(t, "", plan.refusedImage)
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
	// A quote in a writable path would otherwise end the literal early and
	// change which paths the profile allows.
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
	// dropped rule silently widens nothing but denies the write it exists for,
	// and a missing rule is far harder to diagnose than a denial.
	plan := &sandboxPlan{backend: SandboxSeatbelt, network: true, work: "/definitely/not/here"}
	assert.Contains(t, plan.seatbeltWritablePaths(), "/definitely/not/here")
}

func TestSandboxPlanDescribeSeatbelt(t *testing.T) {
	assert.Equal(t, "seatbelt", (&sandboxPlan{backend: SandboxSeatbelt, network: true}).describe())
	assert.Equal(t, "seatbelt (no network)", (&sandboxPlan{backend: SandboxSeatbelt}).describe())
}

func TestResolvConfTargetIsAFileOutsideTheToolTree(t *testing.T) {
	target, ok := resolvConfTarget()
	if !ok {
		// A host whose /etc/resolv.conf is a regular file needs no extra bind:
		// /etc is already in the tool tree.
		return
	}
	assert.False(t, underToolTree(target), "a target inside the tool tree needs no bind of its own")
	info, err := os.Stat(target)
	require.Nil(t, err)
	assert.False(t, info.IsDir(), "the exception is one file, never a host directory tree")
}

func TestBwrapBindsTheResolvConfTargetAndBackendsStayEqual(t *testing.T) {
	// Forces the systemd-resolved shape: /etc/resolv.conf symlinked into /run,
	// which is what CI runners have and what made the first version of the
	// equality test fail there.
	orig := resolvConfTarget
	t.Cleanup(func() { resolvConfTarget = orig })
	const stub = "/run/systemd/resolve/stub-resolv.conf"
	resolvConfTarget = func() (string, bool) { return stub, true }

	plan := &sandboxPlan{
		backend: SandboxBwrap, image: "img", network: true,
		work: "/tmp/dats-1", workdir: "/home/user/project",
	}
	joined := strings.Join(plan.bwrapArgv("true"), " ")
	assert.Contains(t, joined, "--ro-bind-try "+stub+" "+stub,
		"without it a sandboxed command has no DNS at all on a systemd-resolved host")

	// ...and it is the one allowance: everything else still matches docker.
	hostBinds := set.New[string]()
	argv := plan.bwrapArgv("true")
	for i, arg := range argv {
		if i+1 >= len(argv) || underToolTree(argv[i+1]) || argv[i+1] == stub {
			continue
		}
		if arg == "--ro-bind" || arg == "--ro-bind-try" || arg == "--bind" {
			hostBinds.Add(argv[i+1])
		}
	}
	assert.Equal(t, set.Of("/tmp/dats-1", "/home/user/project"), hostBinds)
}

// TestSandboxPlanExposesOnlyTempDirAndCoverDir pins the whole writable
// surface: the file's temp directory, and --coverdir when the run collects
// coverage. There is no third entry and no way to add one -- scratch goes in
// the temp directory (a real filesystem inside every backend), and a command
// that needs the host belongs in a --no-sandbox run, not in a hole punched
// through a sandboxed one.
func TestSandboxPlanExposesOnlyTempDirAndCoverDir(t *testing.T) {
	plan := &sandboxPlan{backend: SandboxBwrap, network: true, work: "/tmp/dats-1", workdir: "/repo"}
	assert.Equal(t, []string{"/tmp/dats-1"}, plan.writablePaths())

	plan.coverDir = "/repo/coverage"
	assert.Equal(t, []string{"/tmp/dats-1", "/repo/coverage"}, plan.writablePaths())
}
