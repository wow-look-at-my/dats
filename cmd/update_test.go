package cmd


import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wow-look-at-my/dats/runner"
)

func setUpdateFlag(t *testing.T, value bool) {
	t.Helper()
	prev := updateGoldens
	updateGoldens = value
	t.Cleanup(func() { updateGoldens = prev })
}

const snapshotDats = `tests:
	- desc: snap
	  cmd: echo hello
	  outputs:
		snapshot: true
`

func TestUpdateFlagRegistration(t *testing.T) {
	f := rootCmd.PersistentFlags().Lookup("update")
	require.NotNil(t, f, "--update must be registered as a persistent flag")
	assert.Equal(t, "", f.Shorthand, "--update must be long-only")
	assert.Equal(t, "false", f.DefValue, "--update must default to off")
}

func TestRunTestsUpdateWritesGoldensAndSummary(t *testing.T) {
	datsFile := writeDats(t, "snap.dats", snapshotDats)
	setUpdateFlag(t, true)

	var out bytes.Buffer
	require.Nil(t, runTests(context.Background(), []string{datsFile}, &out, 0, nil, ""))

	goldenPath := filepath.Join(runner.SnapshotDir(datsFile), "001-snap.stdout.golden")
	content, err := os.ReadFile(goldenPath)
	require.Nil(t, err, "--update must reach the runner and write the golden")
	assert.Equal(t, "hello\n", string(content))
	assert.Contains(t, out.String(), "  # updated golden: "+goldenPath)
	assert.Contains(t, out.String(), "\nUpdated 1 golden file(s)\n")
	assert.NotContains(t, out.String(), "pruned")

	// A second update run changes nothing and stays silent about goldens.
	var out2 bytes.Buffer
	require.Nil(t, runTests(context.Background(), []string{datsFile}, &out2, 0, nil, ""))
	assert.NotContains(t, out2.String(), "Updated")
	assert.NotContains(t, out2.String(), "updated golden")
}

func TestRunTestsUpdateSummaryCountsPrunes(t *testing.T) {
	datsFile := writeDats(t, "snap.dats", snapshotDats)
	dir := runner.SnapshotDir(datsFile)
	stale := filepath.Join(dir, "007-gone.stdout.golden")
	require.Nil(t, os.MkdirAll(dir, 0o755))
	require.Nil(t, os.WriteFile(stale, []byte("stale"), 0o644))
	setUpdateFlag(t, true)

	var out bytes.Buffer
	require.Nil(t, runTests(context.Background(), []string{datsFile}, &out, 0, nil, ""))
	assert.Contains(t, out.String(), "# pruned stale golden: "+stale)
	assert.Contains(t, out.String(), "\nUpdated 1 golden file(s), pruned 1 stale\n")
	_, statErr := os.Stat(stale)
	assert.True(t, os.IsNotExist(statErr))
}

func TestRunTestsWithoutUpdateComparesOnly(t *testing.T) {
	// Flag off (the default): a missing golden is a failure and no summary line appears.
	datsFile := writeDats(t, "snap.dats", snapshotDats)
	setUpdateFlag(t, false)

	var out bytes.Buffer
	err := runTests(context.Background(), []string{datsFile}, &out, 0, nil, "")
	assert.ErrorIs(t, err, errTestsFailed)
	assert.Contains(t, out.String(), "does not exist (run with --update to create it)")
	assert.NotContains(t, out.String(), "Updated")
	_, statErr := os.Stat(runner.SnapshotDir(datsFile))
	assert.True(t, os.IsNotExist(statErr), "compare mode must never create the snapshot directory")
}

func TestSyntaxAcceptsSnapshotFilesAndUpdateFlag(t *testing.T) {
	datsFile := writeDats(t, "snap.dats", snapshotDats)

	t.Cleanup(func() {
		updateGoldens = false
		f := rootCmd.PersistentFlags().Lookup("update")
		f.Changed = false
		require.Nil(t, f.Value.Set("false"))
		rootCmd.SetArgs(nil)
	})
	rootCmd.SetArgs([]string{"syntax", "--update", datsFile})
	require.Nil(t, rootCmd.Execute())

	_, statErr := os.Stat(runner.SnapshotDir(datsFile))
	assert.True(t, os.IsNotExist(statErr), "dats syntax must not touch goldens")
}

func TestExampleSnapshotGoldensInSync(t *testing.T) {
	example := filepath.Join("..", "examples", "snapshot.dats")
	setUpdateFlag(t, false)

	var out bytes.Buffer
	require.Nil(t, runTests(context.Background(), []string{example}, &out, 0, nil, ""), "output:\n%s", out.String())
	assert.Contains(t, out.String(), "6/6 passed")
}

func TestReportsIncludeSnapshotFailures(t *testing.T) {
	datsFile := writeDats(t, "snap.dats", snapshotDats)
	setUpdateFlag(t, false)
	outDir := t.TempDir()
	junitPath := filepath.Join(outDir, "report.xml")
	jsonPath := filepath.Join(outDir, "report.json")
	setReportFlags(t, junitPath, jsonPath)

	var out bytes.Buffer
	err := runTests(context.Background(), []string{datsFile}, &out, 0, nil, "")
	assert.ErrorIs(t, err, errTestsFailed)

	jsonRaw, readErr := os.ReadFile(jsonPath)
	require.Nil(t, readErr)
	var doc map[string]any
	require.Nil(t, json.Unmarshal(jsonRaw, &doc))
	failures := doc["files"].([]any)[0].(map[string]any)["tests"].([]any)[0].(map[string]any)["failures"].([]any)
	require.Len(t, failures, 1)
	assert.Contains(t, failures[0].(string), "snapshot: stdout: golden file")
	assert.Contains(t, failures[0].(string), "does not exist (run with --update to create it)")

	junitRaw, readErr := os.ReadFile(junitPath)
	require.Nil(t, readErr)
	assert.Contains(t, string(junitRaw), "snapshot: stdout: golden file")
}
