package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshotDir(t *testing.T) {
	assert.Equal(t, "examples/demo.snapshots", SnapshotDir("examples/demo.dats"))
	assert.Equal(t, "/abs/path/t.snapshots", SnapshotDir("/abs/path/t.dats"))
	assert.Equal(t, "noext.snapshots", SnapshotDir("noext"))
}

func TestGoldenFileName(t *testing.T) {
	assert.Equal(t, "001-snap.stdout.golden", GoldenFileName(0, "snap", "stdout"))
	assert.Equal(t, "012-x.stderr.golden", GoldenFileName(11, "X", "stderr"))
	assert.Equal(t, "003-greets-greeting-hello-name-alice.stdout.golden",
		GoldenFileName(2, "greets [greeting=hello, name=alice]", "stdout"))
}

func TestSlugifySnapshotName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"uppercase is lowered", "Echo HELLO", "echo-hello"},
		{"matrix label", "greets [greeting=hello, name=alice]", "greets-greeting-hello-name-alice"},
		{"unicode maps to dashes", "héllo wörld", "h-llo-w-rld"},
		{"consecutive junk collapses", "a  --  b!!c", "a-b-c"},
		{"leading and trailing junk trimmed", "  [weird]  ", "weird"},
		{"digits kept", "test 42", "test-42"},
		{"empty falls back", "", "test"},
		{"all junk falls back", "[]=,! ", "test"},
		{"long names truncate to 60 bytes", strings.Repeat("a", 70), strings.Repeat("a", 60)},
		{"truncation cannot leave a trailing dash", strings.Repeat("a", 59) + " b", strings.Repeat("a", 59)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, slugifySnapshotName(tt.input))
		})
	}
}

func TestNormalizeSnapshotText(t *testing.T) {
	base := t.TempDir()
	ctx := &TestContext{
		BaseDir:   base,
		TestIndex: 3,
		SharedDir: filepath.Join(base, "shared"),
	}
	testDir := filepath.Join(base, "test-3")
	in := fmt.Sprintf("out=%s/outputs/f.txt cfg=%s/c.json root=%s tail", testDir, ctx.SharedDir, base)
	assert.Equal(t, "out={testdir}/outputs/f.txt cfg={shareddir}/c.json root={tmproot} tail",
		NormalizeSnapshotText(in, ctx))

	assert.NotContains(t, NormalizeSnapshotText(testDir+" "+ctx.SharedDir, ctx), "{tmproot}/")

	// Trailing newlines and everything else stay byte-exact.
	assert.Equal(t, "plain\n\n", NormalizeSnapshotText("plain\n\n", ctx))
}

// runSnapshotFile runs one .dats file and returns the file result plus the printed output.
func runSnapshotFile(t *testing.T, path string, update bool) (*FileResult, string) {
	t.Helper()
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	r.Update = update
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	return result, buf.String()
}

func TestSnapshotMissingGoldenFails(t *testing.T) {
	path := writeRunnerDats(t, `
tests:
	- desc: snap
	  cmd: echo hello
	  outputs:
		snapshot: true
`)
	result, out := runSnapshotFile(t, path, false)
	assert.Equal(t, 1, result.Failed)
	require.Len(t, result.Results, 1)
	require.Len(t, result.Results[0].Failures, 1)

	goldenPath := filepath.Join(SnapshotDir(path), "001-snap.stdout.golden")
	want := fmt.Sprintf("snapshot: stdout: golden file %s does not exist (run with --update to create it)", goldenPath)
	assert.Equal(t, want, result.Results[0].Failures[0])
	assert.Contains(t, out, "not ok 1 - snap")
	assert.Contains(t, out, "  # "+want)
}

func TestSnapshotMismatchFirstDifference(t *testing.T) {
	cases := []struct {
		name   string
		golden string
		cmd    string
		want   string
	}{
		{"middle line differs", "a\nB\nc\n", `printf 'a\nb\nc\n'`, `line 1: expected "B", got "b"`},
		{"extra lines in actual", "a", `printf 'a\nb'`, `line 1: expected end of output, got "b"`},
		{"extra lines in golden", "a\nb", `printf 'a'`, `line 1: expected "b", got end of output`},
		{"trailing newline only", "hello", "echo hello", "output differs only by a trailing newline"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeRunnerDats(t, `
tests:
	- desc: snap
	  cmd: `+tc.cmd+`
	  outputs:
		snapshot: true
`)
			goldenPath := filepath.Join(SnapshotDir(path), "001-snap.stdout.golden")
			require.Nil(t, os.MkdirAll(SnapshotDir(path), 0o755))
			require.Nil(t, os.WriteFile(goldenPath, []byte(tc.golden), 0o644))

			result, _ := runSnapshotFile(t, path, false)
			require.Len(t, result.Results, 1)
			require.Len(t, result.Results[0].Failures, 1)
			want := fmt.Sprintf("snapshot: stdout: output does not match golden file %s (%s)", goldenPath, tc.want)
			assert.Equal(t, want, result.Results[0].Failures[0])
		})
	}
}

func TestSnapshotUnreadableGoldenFails(t *testing.T) {
	path := writeRunnerDats(t, `
tests:
	- desc: snap
	  cmd: echo hello
	  outputs:
		snapshot: true
`)
	goldenPath := filepath.Join(SnapshotDir(path), "001-snap.stdout.golden")
	require.Nil(t, os.MkdirAll(goldenPath, 0o755))

	result, _ := runSnapshotFile(t, path, false)
	require.Len(t, result.Results, 1)
	require.Len(t, result.Results[0].Failures, 1)
	assert.Contains(t, result.Results[0].Failures[0],
		fmt.Sprintf("snapshot: stdout: reading golden file %s: ", goldenPath))
}

func TestSnapshotPassesWhenGoldenMatches(t *testing.T) {
	path := writeRunnerDats(t, `
tests:
	- desc: snap
	  cmd: printf 'hello\nworld\n'
	  outputs:
		snapshot: true
`)
	require.Nil(t, os.MkdirAll(SnapshotDir(path), 0o755))
	require.Nil(t, os.WriteFile(filepath.Join(SnapshotDir(path), "001-snap.stdout.golden"),
		[]byte("hello\nworld\n"), 0o644))

	result, out := runSnapshotFile(t, path, false)
	assert.Equal(t, 1, result.Passed)
	assert.Equal(t, 0, result.Failed)
	assert.Contains(t, out, "ok 1 - snap")
}

func TestSnapshotUpdateWritesAndLists(t *testing.T) {
	path := writeRunnerDats(t, `
tests:
	- desc: snap
	  cmd: printf 'out\n'; printf 'err\n' >&2
	  outputs:
		snapshot:
			stdout: true
			stderr: true
	- cmd: echo hi
	  outputs:
		snapshot: true
`)
	result, out := runSnapshotFile(t, path, true)
	assert.Equal(t, 2, result.Passed, "output:\n%s", out)

	stdoutGolden := filepath.Join(SnapshotDir(path), "001-snap.stdout.golden")
	stderrGolden := filepath.Join(SnapshotDir(path), "001-snap.stderr.golden")
	cmdGolden := filepath.Join(SnapshotDir(path), "002-echo-hi.stdout.golden")
	assert.Equal(t, []string{stdoutGolden, stderrGolden}, result.Results[0].UpdatedGoldens)
	assert.Equal(t, []string{cmdGolden}, result.Results[1].UpdatedGoldens)
	for _, line := range []string{
		"  # updated golden: " + stdoutGolden,
		"  # updated golden: " + stderrGolden,
		"  # updated golden: " + cmdGolden,
	} {
		assert.Contains(t, out, line)
	}

	content, err := os.ReadFile(stdoutGolden)
	require.Nil(t, err)
	assert.Equal(t, "out\n", string(content))
	content, err = os.ReadFile(stderrGolden)
	require.Nil(t, err)
	assert.Equal(t, "err\n", string(content))

	// A second update run is a complete no-op: identical output, nothing rewritten, nothing listed.
	result2, out2 := runSnapshotFile(t, path, true)
	assert.Equal(t, 2, result2.Passed)
	for i := range result2.Results {
		assert.Empty(t, result2.Results[i].UpdatedGoldens, "instance %d", i)
	}
	assert.NotContains(t, out2, "updated golden")

	// And the ordinary compare run passes against the written goldens.
	result3, _ := runSnapshotFile(t, path, false)
	assert.Equal(t, 2, result3.Passed)
	assert.Equal(t, 0, result3.Failed)
}

func TestSnapshotUpdateRewritesChangedGolden(t *testing.T) {
	path := writeRunnerDats(t, `
tests:
	- desc: snap
	  cmd: echo new-output
	  outputs:
		snapshot: true
`)
	goldenPath := filepath.Join(SnapshotDir(path), "001-snap.stdout.golden")
	require.Nil(t, os.MkdirAll(SnapshotDir(path), 0o755))
	require.Nil(t, os.WriteFile(goldenPath, []byte("old-output\n"), 0o644))

	result, out := runSnapshotFile(t, path, true)
	assert.Equal(t, 1, result.Passed)
	assert.Equal(t, []string{goldenPath}, result.Results[0].UpdatedGoldens)
	assert.Contains(t, out, "  # updated golden: "+goldenPath)

	content, err := os.ReadFile(goldenPath)
	require.Nil(t, err)
	assert.Equal(t, "new-output\n", string(content))
}

func TestSnapshotUpdateSkipsFailingInstance(t *testing.T) {
	path := writeRunnerDats(t, `
tests:
	- desc: snap
	  cmd: echo new-output; exit 3
	  outputs:
		snapshot: true
`)
	goldenPath := filepath.Join(SnapshotDir(path), "001-snap.stdout.golden")
	require.Nil(t, os.MkdirAll(SnapshotDir(path), 0o755))
	require.Nil(t, os.WriteFile(goldenPath, []byte("old-output\n"), 0o644))

	result, out := runSnapshotFile(t, path, true)
	assert.Equal(t, 1, result.Failed)
	require.Len(t, result.Results, 1)
	assert.Empty(t, result.Results[0].UpdatedGoldens)
	assert.NotContains(t, out, "updated golden")
	require.Len(t, result.Results[0].Failures, 1, "no snapshot failure may be added in update mode")
	assert.Contains(t, result.Results[0].Failures[0], "expected exit code 0, got 3")

	content, err := os.ReadFile(goldenPath)
	require.Nil(t, err)
	assert.Equal(t, "old-output\n", string(content), "a failing instance must not rewrite its golden")
}

func TestSnapshotUpdateCreatesMissingGoldenOnly(t *testing.T) {
	// Update mode without a preexisting directory creates it and the golden.
	path := writeRunnerDats(t, `
tests:
	- desc: fresh
	  cmd: echo brand-new
	  outputs:
		snapshot: true
`)
	_, statErr := os.Stat(SnapshotDir(path))
	require.True(t, os.IsNotExist(statErr))

	result, _ := runSnapshotFile(t, path, true)
	assert.Equal(t, 1, result.Passed)
	content, err := os.ReadFile(filepath.Join(SnapshotDir(path), "001-fresh.stdout.golden"))
	require.Nil(t, err)
	assert.Equal(t, "brand-new\n", string(content))
}

func TestSnapshotTimeoutSkipsSnapshot(t *testing.T) {
	path := writeRunnerDats(t, `
tests:
	- desc: slow
	  cmd: sleep 1
	  timeout: 50ms
	  outputs:
		snapshot: true
`)
	result, _ := runSnapshotFile(t, path, false)
	assert.Equal(t, 1, result.Failed)
	require.Len(t, result.Results, 1)
	require.Len(t, result.Results[0].Failures, 1)
	assert.Contains(t, result.Results[0].Failures[0], "timed out after")
	assert.NotContains(t, result.Results[0].Failures[0], "snapshot")
}

func TestSnapshotNormalizedPathsInGolden(t *testing.T) {
	path := writeRunnerDats(t, `
tests:
	- desc: paths
	  cmd: echo "input at {inputs.data.txt} shared at {shared.cfg.txt}"
	  inputs:
		files:
			data.txt: content
	  outputs:
		snapshot: true
`)
	result, _ := runSnapshotFile(t, path, true)
	assert.Equal(t, 1, result.Passed)

	content, err := os.ReadFile(filepath.Join(SnapshotDir(path), "001-paths.stdout.golden"))
	require.Nil(t, err)
	assert.Equal(t, "input at {testdir}/inputs/data.txt shared at {shareddir}/cfg.txt\n", string(content))

	// Re-run without update: a fresh temp root normalizes to the same tokens, so the golden still matches.
	result2, _ := runSnapshotFile(t, path, false)
	assert.Equal(t, 1, result2.Passed)
	assert.Equal(t, 0, result2.Failed)
}

func TestSnapshotPruneStaleGoldens(t *testing.T) {
	path := writeRunnerDats(t, `
tests:
	- desc: kept
	  cmd: echo kept
	  outputs:
		snapshot: true
`)
	dir := SnapshotDir(path)
	require.Nil(t, os.MkdirAll(dir, 0o755))
	stale := filepath.Join(dir, "002-renamed-away.stdout.golden")
	require.Nil(t, os.WriteFile(stale, []byte("stale\n"), 0o644))
	notGolden := filepath.Join(dir, "README.txt")
	require.Nil(t, os.WriteFile(notGolden, []byte("not a golden"), 0o644))

	result, out := runSnapshotFile(t, path, true)
	assert.Equal(t, 1, result.Passed)
	assert.Equal(t, []string{stale}, result.PrunedGoldens)
	assert.Contains(t, out, "# pruned stale golden: "+stale)

	_, statErr := os.Stat(stale)
	assert.True(t, os.IsNotExist(statErr), "the stale golden must be removed")
	_, statErr = os.Stat(notGolden)
	assert.Nil(t, statErr, "non-golden files are never touched")
	_, statErr = os.Stat(dir)
	assert.Nil(t, statErr, "a non-empty directory must survive pruning")
}

func TestSnapshotPruneRemovesEmptyDir(t *testing.T) {
	// When pruning removes the last entries, the directory itself goes too.
	path := writeRunnerDats(t, `
tests:
	- desc: no snapshots here
	  cmd: echo hi
`)
	dir := SnapshotDir(path)
	require.Nil(t, os.MkdirAll(dir, 0o755))
	require.Nil(t, os.WriteFile(filepath.Join(dir, "001-gone.stdout.golden"), []byte("x"), 0o644))
	require.Nil(t, os.WriteFile(filepath.Join(dir, "001-gone.stderr.golden"), []byte("y"), 0o644))

	result, _ := runSnapshotFile(t, path, true)
	assert.Equal(t, 1, result.Passed)
	assert.Len(t, result.PrunedGoldens, 2)
	_, statErr := os.Stat(dir)
	assert.True(t, os.IsNotExist(statErr), "an emptied snapshot directory must be removed")
}

func TestSnapshotPruneSkippedOnSetupFailure(t *testing.T) {
	path := writeRunnerDats(t, `
setup: exit 3
tests:
	- desc: never runs
	  cmd: echo hi
	  outputs:
		snapshot: true
`)
	dir := SnapshotDir(path)
	stale := filepath.Join(dir, "999-stale.stdout.golden")
	require.Nil(t, os.MkdirAll(dir, 0o755))
	require.Nil(t, os.WriteFile(stale, []byte("stale"), 0o644))

	result, _ := runSnapshotFile(t, path, true)
	require.NotNil(t, result.SetupFailure)
	assert.Empty(t, result.PrunedGoldens)
	_, statErr := os.Stat(stale)
	assert.Nil(t, statErr, "nothing may be pruned after a setup failure")
}

func TestSnapshotPruneSkippedWithoutUpdate(t *testing.T) {
	path := writeRunnerDats(t, `
tests:
	- desc: plain
	  cmd: echo hi
`)
	dir := SnapshotDir(path)
	stale := filepath.Join(dir, "999-stale.stdout.golden")
	require.Nil(t, os.MkdirAll(dir, 0o755))
	require.Nil(t, os.WriteFile(stale, []byte("stale"), 0o644))

	result, _ := runSnapshotFile(t, path, false)
	assert.Equal(t, 1, result.Passed)
	assert.Empty(t, result.PrunedGoldens)
	_, statErr := os.Stat(stale)
	assert.Nil(t, statErr, "ordinary runs never prune")
}

func TestSnapshotMatrixInstanceGoldens(t *testing.T) {
	path := writeRunnerDats(t, `
tests:
	- desc: greet
	  cmd: echo "hi {matrix.who}"
	  matrix:
		who:
			- alice
			- bob
	  outputs:
		snapshot: true
`)
	result, _ := runSnapshotFile(t, path, true)
	assert.Equal(t, 2, result.Passed)

	alice := filepath.Join(SnapshotDir(path), "001-greet-who-alice.stdout.golden")
	bob := filepath.Join(SnapshotDir(path), "002-greet-who-bob.stdout.golden")
	content, err := os.ReadFile(alice)
	require.Nil(t, err)
	assert.Equal(t, "hi alice\n", string(content))
	content, err = os.ReadFile(bob)
	require.Nil(t, err)
	assert.Equal(t, "hi bob\n", string(content))

	result2, _ := runSnapshotFile(t, path, false)
	assert.Equal(t, 2, result2.Passed)
	assert.Equal(t, 0, result2.Failed)
}

const snapshotCorpus = `
tests:
	- desc: both streams
	  cmd: printf 'out\n'; printf 'err\n' >&2
	  outputs:
		snapshot:
			stdout: true
			stderr: true
	- desc: greet
	  cmd: echo "hi {matrix.who}"
	  matrix:
		who:
			- alice
			- bob
	  outputs:
		snapshot: true
	- desc: plain
	  cmd: echo plain
	  outputs:
		stdout:
			- plain
`

func TestSnapshotParallelOutputMatchesSerial(t *testing.T) {
	path := writeRunnerDats(t, snapshotCorpus)
	_, updateOut := runSnapshotFile(t, path, true)
	require.Contains(t, updateOut, "updated golden")

	var serial bytes.Buffer
	rs := NewRunner(&serial, false, false, "")
	_, err := rs.RunFile(context.Background(), path)
	require.Nil(t, err)

	var parallel bytes.Buffer
	rp := NewRunner(&parallel, false, false, "")
	_, err = rp.RunFiles(context.Background(), []string{path}, 4)
	require.Nil(t, err)

	require.Contains(t, serial.String(), "4/4 passed")
	assert.Equal(t, serial.String(), parallel.String(),
		"jobs-mode snapshot output must be byte-identical to a serial run")

	// Remove one golden: both modes report the identical missing-golden failure bytes.
	require.Nil(t, os.Remove(filepath.Join(SnapshotDir(path), "002-greet-who-alice.stdout.golden")))
	var serialFail, parallelFail bytes.Buffer
	rsf := NewRunner(&serialFail, false, false, "")
	_, err = rsf.RunFile(context.Background(), path)
	require.Nil(t, err)
	rpf := NewRunner(&parallelFail, false, false, "")
	_, err = rpf.RunFiles(context.Background(), []string{path}, 4)
	require.Nil(t, err)
	require.Contains(t, serialFail.String(), "does not exist (run with --update to create it)")
	assert.Equal(t, serialFail.String(), parallelFail.String())
}

func TestSnapshotParallelUpdateWritesIdenticalGoldens(t *testing.T) {
	serialPath := writeRunnerDats(t, snapshotCorpus)
	parallelPath := writeRunnerDats(t, snapshotCorpus)

	_, _ = runSnapshotFile(t, serialPath, true)

	var buf bytes.Buffer
	rp := NewRunner(&buf, false, false, "")
	rp.Update = true
	_, err := rp.RunFiles(context.Background(), []string{parallelPath}, 4)
	require.Nil(t, err)

	readTree := func(dir string) map[string]string {
		tree := map[string]string{}
		entries, err := os.ReadDir(dir)
		require.Nil(t, err)
		for _, entry := range entries {
			content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			require.Nil(t, err)
			tree[entry.Name()] = string(content)
		}
		return tree
	}
	serialTree := readTree(SnapshotDir(serialPath))
	parallelTree := readTree(SnapshotDir(parallelPath))
	require.NotEmpty(t, serialTree)
	assert.Equal(t, serialTree, parallelTree,
		"jobs-mode --update must write the exact goldens a serial run writes")
}
