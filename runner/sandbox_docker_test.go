package runner

// Sandbox tests for the docker backend, run against a real daemon and
// skipped when there isn't one (or when its image cannot be fetched). The
// backend is a fallback for machines without bubblewrap, so what is asserted
// here is that a .dats file's contract still holds inside a container:
// fixtures, environment, stdin and outputs all arrive, host paths outside the
// temp directory do not, and a timed-out command leaves no container behind.

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireDocker skips unless a daemon is reachable AND the default image can
// actually be run: a runner with docker installed but no registry access must
// skip, not fail -- the missing image says nothing about the code under test.
func requireDocker(t *testing.T) {
	t.Helper()
	if err := probeDocker(); err != nil {
		t.Skipf("docker not usable here: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "docker", "run", "--rm", DefaultSandboxImage, "true").CombinedOutput(); err != nil {
		t.Skipf("cannot run %s: %v: %s", DefaultSandboxImage, err, strings.TrimSpace(string(out)))
	}
}

func TestRunFileDockerSandbox(t *testing.T) {
	requireDocker(t)
	hostProbe := filepath.Join(t.TempDir(), "written.txt")

	path := writeRunnerDats(t, `
shared:
  files:
    shared.txt: shared-content
tests:
  - desc: runs inside the image
    cmd: grep -q debian /etc/os-release
  - desc: fixtures, shared files, env and stdin all arrive
    cmd: 'cat {inputs.in.txt} {shared.shared.txt}; echo "$MY_VAR"; cat'
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
  - desc: the outputs directory is writable and lands on the host
    cmd: echo produced > {outputs.result.txt}
    outputs:
      files:
        result.txt:
          match:
            - produced
  - desc: host paths outside the temp directory are not writable
    cmd: 'echo pwned > `+hostProbe+` 2>/dev/null && echo WROTE || echo BLOCKED'
    outputs:
      stdout:
        - BLOCKED
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	r.Sandbox = NewSandboxConfig(SandboxDocker, "")

	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 4, result.Passed, "output:\n%s", buf.String())
	assert.NoFileExists(t, hostProbe, "a container write must not reach an unmounted host path")
	assert.Contains(t, buf.String(), "# sandbox: docker "+DefaultSandboxImage)
}

func TestRunFileDockerSandboxTimeoutLeavesNoContainer(t *testing.T) {
	// Killing `docker run` kills the client, not the workload: without the
	// plan's kill hook every timed-out test would leak a running container.
	requireDocker(t)
	path := writeRunnerDats(t, `
tests:
  - desc: sleeps past its deadline
    cmd: sleep 120
    timeout: 2s
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	r.Sandbox = NewSandboxConfig(SandboxDocker, "")

	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	require.Equal(t, 1, result.Failed)
	assert.Contains(t, result.Results[0].Failures[0], "timed out")

	// The kill is fired asynchronously as the command is torn down, so give
	// the daemon a moment before demanding the container be gone.
	assert.Eventually(t, func() bool {
		out, err := exec.Command("docker", "ps", "--format", "{{.Names}}").Output()
		return err == nil && !strings.Contains(string(out), "dats-"+strconv.Itoa(os.Getpid())+"-")
	}, 30*time.Second, 500*time.Millisecond, "the timed-out container must not outlive the test")
}
