package runner

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
	  cmd: 'echo pwned | tee `+probe+` || echo BLOCKED'
	  outputs:
		stdout:
			- BLOCKED
	- desc: the test's own outputs directory is writable
	  cmd: echo produced | tee {outputs.result.txt}
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
	requireBwrap(t)
	probe := filepath.Join("/etc", "dats-sandbox-hook-probe.txt")
	t.Cleanup(func() { _ = os.Remove(probe) })

	path := writeRunnerDats(t, `
setup: echo pwned | tee `+probe+`
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

func TestRunFileSandboxFileCannotOptOut(t *testing.T) {
	hostDir := t.TempDir()
	probe := filepath.Join(hostDir, "written.txt")

	path := writeRunnerDats(t, `
sandbox: false
tests:
	- desc: writes to the host
	  cmd: echo produced | tee `+probe+`
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	r.Sandbox = NewSandboxConfig(SandboxBwrap, "")

	_, err := r.RunFile(context.Background(), path)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "a file cannot turn its own sandbox off")
	assert.NoFileExists(t, probe, "a refused file runs nothing")
}

func TestRunFileRunLevelOptOutRunsOnHost(t *testing.T) {
	hostDir := t.TempDir()
	probe := filepath.Join(hostDir, "written.txt")

	path := writeRunnerDats(t, `
tests:
	- desc: writes to the host
	  cmd: echo produced | tee `+probe+`
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	r.Sandbox = NewSandboxConfig(SandboxNone, "")

	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 1, result.Passed, "output:\n%s", buf.String())
	assert.FileExists(t, probe, "an opted-out run runs on the host")
	assert.NotContains(t, buf.String(), "# sandbox:", "an unsandboxed file announces nothing")
}

func TestRunFileSandboxNetworkOff(t *testing.T) {
	requireBwrap(t)
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
	  cmd: echo produced | tee {outputs.result.txt}
	  outputs:
		files:
			result.txt:
				match:
					- produced
	- desc: an undeclared host path is not
	  cmd: 'echo pwned | tee `+probe+` || echo BLOCKED'
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
	// The sandbox sits between dats and the workload, so the timeout path has to kill through it.
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
	requireBwrap(t)
	path := writeRunnerDats(t, `
shared:
	files:
		shared.txt: shared-content
setup: cp {shared.shared.txt} {shared.copied.txt}
tests:
	- desc: fixtures, env, stdin and shared files
	  cmd: |
		cat {inputs.in.txt} {shared.copied.txt}
		echo "$MY_VAR"
		cat
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

func TestRunFilesSandboxed(t *testing.T) {
	requireBwrap(t)
	first := writeRunnerDats(t, `
tests:
	- desc: read-only host
	  cmd: echo x | tee /etc/dats-parallel-probe || echo BLOCKED
	  outputs:
		stdout:
			- BLOCKED
`)
	second := filepath.Join(t.TempDir(), "second.dats")
	require.Nil(t, os.WriteFile(second, []byte(`
tests:
	- desc: writable outputs
	  cmd: echo produced | tee {outputs.out.txt}
	  outputs:
		files:
			out.txt:
				{}
`), 0644))

	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	r.Sandbox = NewSandboxConfig(SandboxBwrap, "")

	results, err := r.RunFiles(context.Background(), []string{first, second}, 2)
	require.Nil(t, err)
	require.Len(t, results, 2)
	for _, result := range results {
		assert.True(t, result.Ok(), "output:\n%s", buf.String())
	}
	assert.Equal(t, 2, strings.Count(buf.String(), "# sandbox: bwrap"))
	assert.NoFileExists(t, "/etc/dats-parallel-probe")
}

// requireSeatbelt skips a test that needs a real macOS sandbox.
func requireSeatbelt(t *testing.T) {
	t.Helper()
	if err := probeSeatbelt(); err != nil {
		t.Skipf("sandbox-exec not usable here: %v", err)
	}
}

func TestRunFileSeatbeltSandbox(t *testing.T) {
	requireSeatbelt(t)
	probe := filepath.Join("/etc", "dats-seatbelt-probe.txt")
	t.Cleanup(func() { _ = os.Remove(probe) })

	path := writeRunnerDats(t, `
tests:
	- desc: the host filesystem is read-only
	  cmd: 'echo pwned | tee `+probe+` || echo BLOCKED'
	  outputs:
		stdout:
			- BLOCKED
	- desc: fixtures, outputs and stdin still work
	  cmd: |
		cp {inputs.in.txt} {outputs.result.txt}
		cat
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
