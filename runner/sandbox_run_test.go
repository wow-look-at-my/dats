package runner

// Sandbox tests that run whole .dats files through a real bubblewrap: what a
// sandboxed command may and may not touch, that the file-level opt-out really
// runs on the host, and that the plumbing dats layers on top (fixtures,
// timeouts, hooks, jobs mode) still works with a sandbox in the middle.
// Skipped when the host cannot provide bubblewrap.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	  cmd: "cut -d: -f1 /proc/net/dev | tail -n +3 | tr -d \" \" | sort | tr \"\\n\" \",\""
	  outputs:
		stdout:
			"0": ^lo,$
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	r.Sandbox = NewSandboxConfig(SandboxBwrap, "")

	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 1, result.Passed, "output:\n%s", buf.String())
	assert.Contains(t, buf.String(), "# sandbox: bwrap (no network)")
}

// TestRunFileSandboxScratchGoesInTheTempDir is the replacement for the
// removed writable-path declaration: a command with something to write has the
// file's own temp directory, on every backend, and a host path it was not
// given stays refused. There is no third option by design -- an escape hatch
// per path is a hole in the isolation whose consequences the file's author
// cannot see, and a command that truly needs the host says `sandbox: false`.
func TestRunFileSandboxScratchGoesInTheTempDir(t *testing.T) {
	requireBwrap(t)
	hostDir := t.TempDir()
	probe := filepath.Join(hostDir, "written.txt")

	path := writeRunnerDats(t, `
tests:
	- desc: scratch goes in the private tmpfs
	  cmd: |
			d="$(mktemp -d)"
			echo produced > "$d/scratch.txt"
			cat "$d/scratch.txt"
	  outputs:
		stdout:
			- produced
	- desc: ...or in the test's own outputs directory
	  cmd: echo produced > {outputs.result.txt}
	  outputs:
		files:
			result.txt:
				match:
					- produced
	- desc: an undeclared host path is not
	  cmd: 'echo pwned > `+probe+` 2>/dev/null && echo WROTE || echo BLOCKED'
	  outputs:
		stdout:
			- BLOCKED
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	r.Sandbox = NewSandboxConfig(SandboxBwrap, "")

	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 3, result.Passed, "output:\n%s", buf.String())
	assert.NoFileExists(t, probe, "no file may declare its way onto the host")
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
	  cmd: cat {inputs.in.txt} {shared.copied.txt}; echo "$MY_VAR"; cat
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
	  cmd: echo x > /etc/dats-parallel-probe 2>/dev/null && echo WROTE || echo BLOCKED
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
			out.txt:
				{}
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

// requireSeatbelt skips a test that needs a real macOS sandbox. On Linux
// sandbox-exec simply does not exist, so this skips everywhere except a mac --
// which is the point: the assertions below are the only thing that can prove
// the generated profile is actually accepted and enforced, and they must run
// on the platform that has the enforcer.
func requireSeatbelt(t *testing.T) {
	t.Helper()
	if err := probeSeatbelt(); err != nil {
		t.Skipf("sandbox-exec not usable here: %v", err)
	}
}

func TestRunFileSeatbeltSandbox(t *testing.T) {
	requireSeatbelt(t)
	// /etc is present and root-owned on macOS too, and the sandbox is what
	// must refuse the write -- exactly the bwrap assertion, one platform over.
	probe := filepath.Join("/etc", "dats-seatbelt-probe.txt")
	t.Cleanup(func() { _ = os.Remove(probe) })

	path := writeRunnerDats(t, `
tests:
  - desc: the host filesystem is read-only
    cmd: 'echo pwned > `+probe+` 2>/dev/null && echo WROTE || echo BLOCKED'
    outputs:
      stdout:
        - BLOCKED
  - desc: fixtures, outputs and stdin still work
    cmd: 'cat {inputs.in.txt} > {outputs.result.txt}; cat'
    inputs:
      stdin: from-stdin
      files:
        in.txt: input-content
    outputs:
      stdout:
        - from-stdin
      files:
        result.txt:
          match:
            - input-content
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	r.Sandbox = NewSandboxConfig(SandboxSeatbelt, "")

	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 2, result.Passed, "output:\n%s", buf.String())
	assert.NoFileExists(t, probe, "the sandboxed write must not reach the host")
	assert.Contains(t, buf.String(), "# sandbox: seatbelt")
}

func TestRunFileSeatbeltWritablePath(t *testing.T) {
	requireSeatbelt(t)
	hostDir := t.TempDir()
	probe := filepath.Join(hostDir, "written.txt")

	path := writeRunnerDats(t, `
sandbox:
  writable:
    - `+hostDir+`
tests:
  - desc: a declared host path stays writable
    cmd: echo produced > `+probe+`
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	r.Sandbox = NewSandboxConfig(SandboxSeatbelt, "")

	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 1, result.Passed, "output:\n%s", buf.String())
	assert.FileExists(t, probe)
}
