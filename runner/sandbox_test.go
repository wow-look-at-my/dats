package runner

// Sandbox tests.

import (
	"errors"
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

func requireBwrap(t *testing.T) {
	t.Helper()
	if _, err := probeBwrap(); err != nil {
		t.Skipf("bubblewrap not usable here: %v", err)
	}
}

// sandboxConfigWithProbe builds a config whose backend probes are answered by probe instead of the host.
func sandboxConfigWithProbe(mode SandboxMode, probe func(SandboxMode) (procMode, error)) *SandboxConfig {
	cfg := NewSandboxConfig(mode, "")
	cfg.probe = probe
	return cfg
}

func probeAlways(err error) func(SandboxMode) (procMode, error) {
	return func(SandboxMode) (procMode, error) { return procFresh, err }
}

// An NT host has no backend to install: the marker is what lets a library
// caller say so and run on the host, while a missing bwrap on linux stays the
// ordinary error a caller must not paper over.
func TestSandboxBackendMarksAHostThatCanNeverSandbox(t *testing.T) {
	t.Run("an NT host carries the marker", func(t *testing.T) {
		forceHostGOOS(t, "windows")
		cfg := sandboxConfigWithProbe(SandboxAuto, probeAlways(assertError("docker: the daemon serves windows containers")))
		_, err := cfg.Backend()
		require.NotNil(t, err)
		assert.True(t, errors.Is(err, ErrNoBackendOnHost))
		assert.Contains(t, err.Error(), "windows containers", "the probe's own reason must survive the wrap")
	})

	t.Run("a linux host missing bwrap does not", func(t *testing.T) {
		forceHostGOOS(t, "linux")
		cfg := sandboxConfigWithProbe(SandboxAuto, probeAlways(assertError("bwrap: not found in $PATH")))
		_, err := cfg.Backend()
		require.NotNil(t, err)
		assert.False(t, errors.Is(err, ErrNoBackendOnHost), "installing bubblewrap fixes this, so it must stay fatal")
	})
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
	assert.Less(t, strings.Index(joined, "--tmpfs /tmp"), strings.Index(joined, "--bind /tmp/dats-1"))
	assert.Less(t, strings.Index(joined, "--ro-bind-try /usr /usr"), strings.Index(joined, "--tmpfs /tmp"))
	assert.Less(t, strings.Index(joined, "--ro-bind-try /home/user/project"), strings.Index(joined, "--bind /srv/coverage"))
	// ...and the --chdir into it comes after the bind that creates it.
	assert.Less(t, strings.Index(joined, "--ro-bind-try /home/user/project"), strings.Index(joined, "--chdir /home/user/project"))
}

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
			assert.False(t, strings.HasPrefix(src, "/home/") || strings.HasPrefix(src, "/root"),
				"bwrap bound host data path %q", src)
		}
	}
}

// TestToolTreeCoversAddOnToolchains pins /opt in the tool tree.
func TestToolTreeCoversAddOnToolchains(t *testing.T) {
	assert.Contains(t, toolTreePaths, "/opt")

	plan := &sandboxPlan{backend: SandboxBwrap, network: true, work: "/tmp/dats-1", workdir: "/home/user/project"}
	assert.Contains(t, strings.Join(plan.bwrapArgv("true"), " "), "--ro-bind-try /opt /opt")

	// It stays a tool tree, not a separate host: read-only, and never a bind of anything holding user data.
	assert.True(t, underToolTree("/opt/hostedtoolcache/go/1.25.0/x64/bin/go"))
	assert.False(t, underToolTree("/home/runner/work"))
}

func TestBwrapAndDockerExposeTheSameHostPaths(t *testing.T) {
	plan := &sandboxPlan{
		backend:  SandboxBwrap,
		image:    "img",
		network:  true,
		work:     "/tmp/dats-1",
		coverDir: "/srv/coverage",
		workdir:  "/home/user/project",
	}

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
	t.Setenv("DATS_TEST_HANDOFF_DIR", "/home/user/project/build/.stage")
	plan := &sandboxPlan{backend: SandboxDocker, image: "img", network: true, work: "/tmp/dats-1"}
	joined := strings.Join(plan.dockerArgv("n", "true", []string{"FOO=bar"}), " ")

	assert.Contains(t, joined, "-e DATS_TEST_HANDOFF_DIR=/home/user/project/build/.stage")
	assert.Contains(t, joined, "-e FOO=bar")
	// dats' own additions come last, so a test's inputs.env wins over an inherited variable of the same name.
	assert.Less(t, strings.Index(joined, "-e DATS_TEST_HANDOFF_DIR"), strings.Index(joined, "-e FOO=bar"))
}

// The fallback /proc shape swaps a single argument and keeps everything that contains the command.
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

	assert.Equal(t, drop(fresh, "--proc", "/proc"), drop(shared, "--ro-bind", "/proc", "/proc"),
		"the two shapes must differ ONLY in how /proc is provided")
}

// drop returns argv without the earliest run of the given consecutive arguments.
func drop(argv []string, run ...string) []string {
	for i := range argv {
		if i+len(run) <= len(argv) && slices.Equal(argv[i:i+len(run)], run) {
			return slices.Concat(argv[:i:i], argv[i+len(run):])
		}
	}
	return argv
}

func TestProbeBwrapPrefersThePrivateProcfs(t *testing.T) {
	requireBwrap(t)
	path, err := exec.LookPath("bwrap")
	require.Nil(t, err)

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

// A reduced sandbox is announced on every file it applies to, and explained a single time.
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

// The sandbox must ask for a USER namespace, and ask for it before the namespaces nested inside it.
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

	r = &Runner{Sandbox: sandboxConfigWithProbe(SandboxNone, probeAlways(nil))}
	plan, err = r.newSandboxPlan(&schema.SandboxSpec{Network: &network}, "/tmp/w")
	require.Nil(t, err)
	assert.Nil(t, plan)
}

func TestNewSandboxPlanOptOutNeedsNoBackend(t *testing.T) {
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
	assert.Contains(t, plan.writablePaths(), r.CoverDir)
	assert.Equal(t, "docker file:tag (no network)", plan.describe())
}

func TestNewSandboxPlanImagePrecedence(t *testing.T) {
	newRunner := func(operatorImage string) *Runner {
		r := &Runner{Sandbox: sandboxConfigWithProbe(SandboxDocker, probeAlways(nil))}
		r.Sandbox.Image = operatorImage
		return r
	}

	// Operator pinned an image, file wants another: the operator's wins, out loud.
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

	// Neither named an image: the default, and nothing to announce.
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

func TestSeatbeltArgvPointsTmpdirInsideTheWritableSet(t *testing.T) {
	plan := &sandboxPlan{backend: SandboxSeatbelt, network: true, work: "/tmp/dats-1", tmp: "/tmp/dats-1/" + sandboxTmpDirName}
	argv := plan.seatbeltArgv("echo hi")

	assert.Equal(t, []string{
		"env",
		"TMPDIR=/tmp/dats-1/" + sandboxTmpDirName,
		"TMP=/tmp/dats-1/" + sandboxTmpDirName,
		"TEMP=/tmp/dats-1/" + sandboxTmpDirName,
		"bash", "-c", "echo hi",
	}, argv[3:], "the host TMPDIR is outside the writable set, so a command that inherits it writes nowhere")
	assert.Contains(t, argv[2], `(subpath "/tmp/dats-1")`,
		"the scratch directory needs no rule of its own: work already covers it")
}

func TestNewSandboxPlanGivesSeatbeltAWritableTmpdir(t *testing.T) {
	work := t.TempDir()
	r := &Runner{Sandbox: sandboxConfigWithProbe(SandboxSeatbelt, probeAlways(nil))}

	plan, err := r.newSandboxPlan(nil, work)
	require.Nil(t, err)
	require.NotNil(t, plan)

	assert.Equal(t, filepath.Join(work, sandboxTmpDirName), plan.tmp)
	info, err := os.Stat(plan.tmp)
	require.Nil(t, err, "the directory exists before any command runs")
	assert.True(t, info.IsDir())
}

func TestSeatbeltProfileShape(t *testing.T) {
	profile := seatbeltProfile([]string{"/tmp/dats-1", "/var/data"}, true)

	assert.Contains(t, profile, "(version 1)")
	assert.Contains(t, profile, `(subpath "/tmp/dats-1")`)
	assert.Contains(t, profile, `(subpath "/var/data")`)
	assert.Contains(t, profile, `(literal "/dev/null")`, "a shell that cannot write /dev/null is not a usable shell")
	assert.NotContains(t, profile, "(deny network*)", "the network stays on unless a file turns it off")

	allowAll := strings.Index(profile, "(allow default)")
	denyWrites := strings.Index(profile, "(deny file-write*)")
	allowWrites := strings.Index(profile, "(allow file-write*")
	assert.Less(t, allowAll, denyWrites)
	assert.Less(t, denyWrites, allowWrites)
}

func TestSeatbeltProfileNetworkOff(t *testing.T) {
	profile := seatbeltProfile([]string{"/tmp/dats-1"}, false)
	assert.Contains(t, profile, "(deny network*)")
	assert.Less(t, strings.Index(profile, "(allow default)"), strings.Index(profile, "(deny network*)"))
}

func TestSeatbeltProfileEscapesPaths(t *testing.T) {
	profile := seatbeltProfile([]string{`/tmp/we"ird\path`}, true)
	assert.Contains(t, profile, `(subpath "/tmp/we\"ird\\path")`)
}

func TestSeatbeltWritablePathsResolveSymlinks(t *testing.T) {
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
		// A host whose /etc/resolv.conf is a regular file needs no extra bind: /etc is already in the tool tree.
		return
	}
	assert.False(t, underToolTree(target), "a target inside the tool tree needs no bind of its own")
	info, err := os.Stat(target)
	require.Nil(t, err)
	assert.False(t, info.IsDir(), "the exception is one file, never a host directory tree")
}

func TestBwrapBindsTheResolvConfTargetAndBackendsStayEqual(t *testing.T) {
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

	// ...and it is the only allowance: everything else still matches docker.
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

func TestSandboxPlanExposesOnlyTempDirAndCoverDir(t *testing.T) {
	plan := &sandboxPlan{backend: SandboxBwrap, network: true, work: "/tmp/dats-1", workdir: "/repo"}
	assert.Equal(t, []string{"/tmp/dats-1"}, plan.writablePaths())

	plan.coverDir = "/repo/coverage"
	assert.Equal(t, []string{"/tmp/dats-1", "/repo/coverage"}, plan.writablePaths())
}
