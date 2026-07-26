package runner

// Sandbox tests. The argv builders and backend resolution are tested with
// injected probes (no backend needed); the isolation itself is tested against
// a real bubblewrap, skipped when the host cannot provide one.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wow-look-at-my/dats/schema"
)

// requireBwrap skips a test that needs real isolation when bubblewrap is
// missing or the kernel denies it (containers routinely do). The tests below
// are still worth having: CI installs bubblewrap so they run there.
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
	for _, name := range []string{"auto", "bwrap", "docker", "none"} {
		mode, err := ParseSandboxMode(name)
		require.Nil(t, err)
		assert.Equal(t, SandboxMode(name), mode)
	}
	_, err := ParseSandboxMode("firejail")
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "auto, bwrap, docker, or none")
}

func TestSandboxBackendAutoPrefersBwrap(t *testing.T) {
	cfg := sandboxConfigWithProbe(SandboxAuto, probeAlways(nil))
	backend, err := cfg.Backend()
	require.Nil(t, err)
	assert.Equal(t, SandboxBwrap, backend)
}

func TestSandboxBackendAutoFallsBackToDocker(t *testing.T) {
	cfg := sandboxConfigWithProbe(SandboxAuto, func(mode SandboxMode) error {
		if mode == SandboxBwrap {
			return assertError("bwrap: not found in $PATH")
		}
		return nil
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

func TestRunFileWithoutUsableBackendFails(t *testing.T) {
	// The file needs a sandbox, none can be provided: the file fails outright
	// rather than running its commands on the host.
	path := writeRunnerDats(t, `
tests:
  - desc: never runs
    cmd: echo hi
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	r.Sandbox = sandboxConfigWithProbe(SandboxAuto, probeAlways(assertError("unavailable")))

	result, err := r.RunFile(context.Background(), path)
	require.NotNil(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no usable sandbox backend")
	assert.Contains(t, err.Error(), "--no-sandbox")
	assert.NotContains(t, buf.String(), "ok 1", "no command may run when the sandbox is missing")
}

func TestRunFileSandboxBlocksHostWrites(t *testing.T) {
	requireBwrap(t)
	probe := filepath.Join("/etc", "dats-sandbox-probe.txt")
	t.Cleanup(func() { _ = os.Remove(probe) })

	path := writeRunnerDats(t, `
tests:
  - desc: host filesystem is read-only
    cmd: 'echo pwned > `+probe+` 2>/dev/null && echo WROTE || echo BLOCKED'
    outputs:
      stdout:
        - BLOCKED
  - desc: the test's own outputs directory is writable
    cmd: echo produced > {outputs.result.txt}
    outputs:
      files:
        result.txt:
          match:
            - produced
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	r.Sandbox = NewSandboxConfig(SandboxBwrap, "")

	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 2, result.Passed, "output:\n%s", buf.String())
	assert.NoFileExists(t, probe, "the sandboxed write must not reach the host")
	assert.Contains(t, buf.String(), "# sandbox: bwrap", "a sandboxed run says so")
}

func TestRunFileSandboxHooksAreSandboxedToo(t *testing.T) {
	// Setup and teardown are commands from the same file and get the same
	// sandbox: a file cannot use its hooks as an unsandboxed side door.
	requireBwrap(t)
	probe := filepath.Join("/etc", "dats-sandbox-hook-probe.txt")
	t.Cleanup(func() { _ = os.Remove(probe) })

	path := writeRunnerDats(t, `
setup: echo pwned > `+probe+`
tests:
  - desc: fails with the file's setup
    cmd: echo hi
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	r.Sandbox = NewSandboxConfig(SandboxBwrap, "")

	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	require.NotNil(t, result.SetupFailure, "the sandboxed setup write must fail:\n%s", buf.String())
	assert.NoFileExists(t, probe)
	assert.Equal(t, 1, result.Failed)
}

func TestRunFileSandboxFileOptOutRunsOnHost(t *testing.T) {
	requireBwrap(t)
	hostDir := t.TempDir()
	probe := filepath.Join(hostDir, "written.txt")

	path := writeRunnerDats(t, `
sandbox: false
tests:
  - desc: writes to the host
    cmd: echo produced > `+probe+`
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	r.Sandbox = NewSandboxConfig(SandboxBwrap, "")

	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 1, result.Passed, "output:\n%s", buf.String())
	assert.FileExists(t, probe, "an opted-out file runs on the host")
	assert.NotContains(t, buf.String(), "# sandbox:", "an unsandboxed file announces nothing")
}

func TestRunFileSandboxNetworkOff(t *testing.T) {
	requireBwrap(t)
	// /proc is mounted inside the sandbox's namespaces, so /proc/net/dev is
	// the network namespace's own view: loopback and nothing else.
	path := writeRunnerDats(t, `
sandbox:
  network: false
tests:
  - desc: only loopback exists
    cmd: 'cut -d: -f1 /proc/net/dev | tail -n +3 | tr -d " " | sort | tr "\n" ","'
    outputs:
      stdout:
        0: "^lo,$"
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	r.Sandbox = NewSandboxConfig(SandboxBwrap, "")

	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 1, result.Passed, "output:\n%s", buf.String())
	assert.Contains(t, buf.String(), "# sandbox: bwrap (no network)")
}

func TestRunFileSandboxWritablePath(t *testing.T) {
	requireBwrap(t)
	hostDir := t.TempDir()
	probe := filepath.Join(hostDir, "written.txt")

	path := writeRunnerDats(t, `
sandbox:
  writable:
    - `+hostDir+`
tests:
  - desc: declared host path stays writable
    cmd: echo produced > `+probe+`
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	r.Sandbox = NewSandboxConfig(SandboxBwrap, "")

	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 1, result.Passed, "output:\n%s", buf.String())
	assert.FileExists(t, probe)
}

func TestRunFileSandboxTimeoutStillTimesOut(t *testing.T) {
	// The sandbox sits between dats and the workload, so the timeout path has
	// to kill through it. (bwrap reports a signalled child as exit 128+n, so
	// a broken kill would show up as an ordinary exit-code failure instead of
	// a timeout.)
	requireBwrap(t)
	path := writeRunnerDats(t, `
tests:
  - desc: sleeps past its deadline
    cmd: sleep 30
    timeout: 500ms
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	r.Sandbox = NewSandboxConfig(SandboxBwrap, "")

	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	require.Equal(t, 1, result.Failed)
	require.Len(t, result.Results[0].Failures, 1)
	assert.Contains(t, result.Results[0].Failures[0], "timed out")
}

func TestRunFileSandboxInputsEnvAndStdinStillWork(t *testing.T) {
	// Fixtures live in the sandbox's writable work directory and the
	// environment passes through: the placeholder plumbing must be unaffected.
	requireBwrap(t)
	path := writeRunnerDats(t, `
shared:
  files:
    shared.txt: shared-content
setup: cp {shared.shared.txt} {shared.copied.txt}
tests:
  - desc: fixtures, env, stdin and shared files
    cmd: 'cat {inputs.in.txt} {shared.copied.txt}; echo "$MY_VAR"; cat'
    inputs:
      stdin: from-stdin
      files:
        in.txt: input-content
      env:
        MY_VAR: from-env
    outputs:
      stdout:
        - input-content
        - shared-content
        - from-env
        - from-stdin
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	r.Sandbox = NewSandboxConfig(SandboxBwrap, "")

	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 1, result.Passed, "output:\n%s", buf.String())
}

func TestRunFilesParallelSandboxed(t *testing.T) {
	// Jobs mode gives each file its own Runner; the sandbox config (and its
	// memoized backend) has to reach every one of them.
	requireBwrap(t)
	first := writeRunnerDats(t, `
tests:
  - desc: read-only host
    cmd: 'echo x > /etc/dats-parallel-probe 2>/dev/null && echo WROTE || echo BLOCKED'
    outputs:
      stdout:
        - BLOCKED
`)
	second := filepath.Join(t.TempDir(), "second.dats")
	require.Nil(t, os.WriteFile(second, []byte(`
tests:
  - desc: writable outputs
    cmd: echo produced > {outputs.out.txt}
    outputs:
      files:
        out.txt: {}
`), 0644))

	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	r.Sandbox = NewSandboxConfig(SandboxBwrap, "")

	results, err := r.RunFilesParallel(context.Background(), []string{first, second}, 2)
	require.Nil(t, err)
	require.Len(t, results, 2)
	for _, result := range results {
		assert.True(t, result.Ok(), "output:\n%s", buf.String())
	}
	assert.Equal(t, 2, strings.Count(buf.String(), "# sandbox: bwrap"))
	assert.NoFileExists(t, "/etc/dats-parallel-probe")
}

// assertError is a tiny error constructor for the injected probes.
type assertError string

func (e assertError) Error() string { return string(e) }
