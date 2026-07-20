//go:build linux

package runner

// Priority probes: jobs mode runs every workload command -- test instances
// AND file-level setup/teardown hooks -- at nice 19, while serial mode never
// touches priority. Each spawned command reports its own niceness with the
// coreutils `nice` command (no arguments prints the current niceness), which
// pairs with the linux getpriority conventions below; hence linux-only.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// niceProbe prints the calling process's nice value, e.g. "19". The `nice`
// command is used rather than /proc/self/status because sandboxed kernels
// may omit the status file's Nice: field while still honoring setpriority.
const niceProbe = `nice`

// ownNice returns this test process's nice value. The raw linux getpriority
// syscall returns 20-nice (so it never returns a negative value that could
// be mistaken for an errno); undo that to get the plain nice value.
func ownNice(t *testing.T) string {
	t.Helper()
	prio, err := syscall.Getpriority(syscall.PRIO_PROCESS, 0)
	require.Nil(t, err)
	return strconv.Itoa(20 - prio)
}

// TestParallelRunsWorkloadsAtLowPriority proves jobs mode renices what it
// spawns: the test command observes nice 19 directly, the setup hook's
// niceness is captured into a shared file and asserted by a test, and the
// teardown hook's niceness is captured to a path the Go test reads
// afterwards. Hooks and instances share one execution path, so all three
// must see 19.
func TestParallelRunsWorkloadsAtLowPriority(t *testing.T) {
	teardownNice := filepath.Join(t.TempDir(), "teardown-nice.txt")
	path := writeParallelDats(t, "nice.dats", `
setup: `+niceProbe+` > {shared.setup-nice.txt}
teardown: `+niceProbe+` > `+teardownNice+`
tests:
  - desc: test command runs at nice 19
    cmd: `+niceProbe+`
    outputs:
      stdout:
        0: "^19$"
  - desc: setup hook ran at nice 19
    cmd: cat {shared.setup-nice.txt}
    outputs:
      stdout:
        0: "^19$"
`)

	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	results, err := r.RunFilesParallel(context.Background(), []string{path}, 2)
	require.Nil(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Ok(), "output:\n%s", buf.String())
	assert.Equal(t, 2, results[0].Passed)

	seen, err := os.ReadFile(teardownNice)
	require.Nil(t, err)
	assert.Equal(t, "19", strings.TrimSpace(string(seen)), "teardown hook must run at nice 19")
}

// TestSerialRunsWorkloadsAtOwnPriority is the control: without -j nothing is
// reniced -- commands and hooks inherit the runner's own priority, and no
// priority syscall is made. When the test process itself already runs at
// nice 19 the two modes are indistinguishable, so the control is skipped.
func TestSerialRunsWorkloadsAtOwnPriority(t *testing.T) {
	nice := ownNice(t)
	if nice == "19" {
		t.Skip("test process already runs at nice 19; serial control is indistinguishable from jobs mode")
	}
	path := writeParallelDats(t, "nice-serial.dats", `
setup: `+niceProbe+` > {shared.setup-nice.txt}
tests:
  - desc: test command inherits the parent priority
    cmd: `+niceProbe+`
    outputs:
      stdout:
        0: "^`+nice+`$"
  - desc: setup hook inherited the parent priority
    cmd: cat {shared.setup-nice.txt}
    outputs:
      stdout:
        0: "^`+nice+`$"
`)

	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.True(t, result.Ok(), "output:\n%s", buf.String())
	assert.Equal(t, 2, result.Passed)
}
