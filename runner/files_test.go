package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeParallelDats writes a single generated .dats file into its own temp dir and returns its path.
func writeParallelDats(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.Nil(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

func TestRunFilesBarriers(t *testing.T) {
	const instances = 8
	var paths []string
	var markerDirs []string
	for f := 0; f < 3; f++ {
		markerDir := t.TempDir()
		content := fmt.Sprintf(`
setup: sleep 0.3 && echo ready > {shared.gate.txt}
teardown: test "$(ls %[1]s | wc -l)" -eq %[2]d
tests:
	- desc: gated instance
	  cmd: grep -q ready {shared.gate.txt} && touch %[1]s/m-{matrix.i}.txt
	  matrix:
		i:
			- 1
			- 2
			- 3
			- 4
			- 5
			- 6
			- 7
			- 8
`, markerDir, instances)
		paths = append(paths, writeParallelDats(t, fmt.Sprintf("barrier-%d.dats", f), content))
		markerDirs = append(markerDirs, markerDir)
	}

	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	results, err := r.RunFiles(context.Background(), paths, 8)
	require.Nil(t, err)
	require.Len(t, results, 3)
	for i, res := range results {
		assert.True(t, res.Ok(), "file %d must pass wholesale, output:\n%s", i, buf.String())
		assert.Equal(t, instances, res.Passed, "file %d", i)
		assert.Equal(t, 0, res.Failed, "file %d", i)
		assert.Empty(t, res.TeardownFailures, "file %d", i)
		entries, readErr := os.ReadDir(markerDirs[i])
		require.Nil(t, readErr)
		assert.Len(t, entries, instances, "file %d must have run every instance", i)
	}
}

func TestRunFilesSetupFailureFailsEveryInstance(t *testing.T) {
	dir := t.TempDir()
	ranMarker := filepath.Join(dir, "ran.txt")
	teardownMarker := filepath.Join(dir, "teardown.txt")
	path := writeParallelDats(t, "setupfail.dats", `
setup: exit 3
teardown: touch `+teardownMarker+`
tests:
	- desc: never runs
	  cmd: touch `+ranMarker+`
	  matrix:
		i: [1, 2, 3]
`)

	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	results, err := r.RunFiles(context.Background(), []string{path}, 4)
	require.Nil(t, err)
	require.Len(t, results, 1)
	res := results[0]

	require.NotNil(t, res.SetupFailure)
	assert.Contains(t, res.SetupFailure.Detail, "exit code 3")
	assert.Equal(t, 0, res.Passed)
	assert.Equal(t, 3, res.Failed)
	require.Len(t, res.Results, 3)
	for _, tr := range res.Results {
		assert.False(t, tr.Passed)
		require.Len(t, tr.Failures, 1)
		// The exact reason string the serial path reports.
		assert.Equal(t, "file setup failed", tr.Failures[0])
	}
	assert.False(t, res.Ok())

	_, statErr := os.Stat(ranMarker)
	assert.True(t, os.IsNotExist(statErr), "no instance command may run after a setup failure")
	_, statErr = os.Stat(teardownMarker)
	assert.Nil(t, statErr, "teardown must still run after a setup failure")

	out := buf.String()
	assert.Contains(t, out, "# setup command failed: exit 3")
	assert.Contains(t, out, "not ok 3 - never runs [i=3]")
	assert.NotContains(t, out, "skip")
}

func TestRunFilesCrossFileConcurrency(t *testing.T) {
	dir := t.TempDir()
	aMarker := filepath.Join(dir, "a.txt")
	bMarker := filepath.Join(dir, "b.txt")
	wait := func(marker string) string {
		return fmt.Sprintf(
			`ok=1; for i in $(seq 100); do if [ -f %s ]; then ok=0; break; fi; sleep 0.1; done; [ "$ok" -eq 0 ]`,
			marker)
	}
	fileA := writeParallelDats(t, "a.dats", fmt.Sprintf(`
tests:
	- desc: writes a, waits for b
	  cmd: touch %s; %s
`, aMarker, wait(bMarker)))
	fileB := writeParallelDats(t, "b.dats", fmt.Sprintf(`
tests:
	- desc: waits for a, writes b
	  cmd: %s && touch %s
`, wait(aMarker), bMarker))

	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	results, err := r.RunFiles(context.Background(), []string{fileA, fileB}, 4)
	require.Nil(t, err)
	require.Len(t, results, 2)
	for i, res := range results {
		assert.True(t, res.Ok(), "file %d rendezvous failed -- files did not overlap, output:\n%s", i, buf.String())
	}
}

func TestOneJobRunsAFilesInstancesInDeclarationOrder(t *testing.T) {
	log := filepath.Join(t.TempDir(), "order.txt")
	// The slow instance sits at the top, so a scheduler that picks the winner
	// reorders it. Waiting for a free slot keeps the declared order.
	path := writeParallelDats(t, "order.dats", fmt.Sprintf(`
tests:
	- desc: records its own position
	  cmd: if [ {matrix.i} = 1 ]; then sleep 0.3; fi; echo {matrix.i} >> %s
	  matrix:
		i:
			- 1
			- 2
			- 3
			- 4
			- 5
			- 6
			- 7
			- 8
`, log))

	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	results, err := r.RunFiles(context.Background(), []string{path}, 1)
	require.Nil(t, err)
	require.Len(t, results, 1)
	require.True(t, results[0].Ok(), "output:\n%s", buf.String())

	recorded, readErr := os.ReadFile(log)
	require.Nil(t, readErr)
	assert.Equal(t, "1\n2\n3\n4\n5\n6\n7\n8\n", string(recorded),
		"-j1 names a sequential run, so one file's instances must run in declaration order")
}

func TestRunFilesParseErrorFailsFast(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran.txt")
	good := writeParallelDats(t, "good.dats", `
tests:
	- cmd: touch `+marker+`
`)
	bad := writeParallelDats(t, "bad.dats", `
tests:
	- cmd: echo hi
	  bogus_key: nope
`)

	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	results, err := r.RunFiles(context.Background(), []string{good, bad}, 2)
	require.NotNil(t, err)
	assert.Nil(t, results)
	assert.Contains(t, err.Error(), "bad.dats")
	_, statErr := os.Stat(marker)
	assert.True(t, os.IsNotExist(statErr), "no command may run when any file fails to parse")
	assert.Equal(t, "", buf.String(), "nothing may be printed when parsing fails")
}

func TestRunFilesRejectsNonPositiveJobs(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	_, err := r.RunFiles(context.Background(), nil, 0)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "at least 1")
}

func TestRunFilesCanceledContextTeardownStillRuns(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "teardown-ran")
	path := writeParallelDats(t, "cancel.dats", `
teardown: touch `+marker+`
tests:
	- desc: long sleeper
	  cmd: sleep 5
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	results, err := r.RunFiles(ctx, []string{path}, 2)
	elapsed := time.Since(start)
	require.Nil(t, err)
	require.Len(t, results, 1)
	assert.Less(t, elapsed, 2*time.Second, "cancellation must abort the run promptly")
	assert.Equal(t, 1, results[0].Failed)
	assert.False(t, results[0].Ok())
	assert.FileExists(t, marker, "teardown must run under context.WithoutCancel even after cancellation")
}
