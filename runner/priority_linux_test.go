//go:build linux

package runner

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

// niceProbe prints the calling process's nice value.
const niceProbe = `nice`

// ownNice returns this test process's nice value.
func ownNice(t *testing.T) string {
	t.Helper()
	prio, err := syscall.Getpriority(syscall.PRIO_PROCESS, 0)
	require.Nil(t, err)
	return strconv.Itoa(20 - prio)
}

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
	results, err := r.RunFiles(context.Background(), []string{path}, 2)
	require.Nil(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Ok(), "output:\n%s", buf.String())
	assert.Equal(t, 2, results[0].Passed)

	seen, err := os.ReadFile(teardownNice)
	require.Nil(t, err)
	assert.Equal(t, "19", strings.TrimSpace(string(seen)), "teardown hook must run at nice 19")
}

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
