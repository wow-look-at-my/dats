package runner

// Tests for file-level setup, teardown, and shared fixtures: shared files are
// written before setup, setup runs before any test, a setup failure fails
// every test in the file (never "skips" them), and teardown always runs.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeRunnerDats writes content to a temp .dats file and returns its path
// (shared by the hooks and matrix RunFile-level tests).
func writeRunnerDats(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runner.dats")
	require.Nil(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

func TestRunFileSharedPlaceholderInCmdContentsAndEnv(t *testing.T) {
	// {shared.X} expands exactly where {inputs.X}/{outputs.X} already do:
	// the command, inputs.files contents, and inputs.env values.
	path := writeRunnerDats(t, `
shared:
	files:
		config.json: "{\"debug\": true}"
tests:
	- desc: cmd expansion
	  cmd: cat {shared.config.json}
	  outputs:
		stdout:
			- "\"debug\": true"
	- desc: input contents and env expansion
	  cmd: diff "$SHARED_PATH" {shared.config.json} && cat {inputs.pointer.txt}
	  inputs:
		files:
			pointer.txt: "{shared.config.json}"
		env:
			SHARED_PATH: "{shared.config.json}"
	  outputs:
		stdout:
			- /shared/config.json
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 2, result.Passed, "output:\n%s", buf.String())
	assert.True(t, result.Ok())
}

func TestRunFileStdinNotExpanded(t *testing.T) {
	// inputs.stdin is passed to the command verbatim: no placeholder
	// namespace, including {shared.X}, is expanded in it.
	path := writeRunnerDats(t, `
shared:
	files:
		config.json: content
tests:
	- cmd: cat
	  inputs:
		stdin: "{shared.config.json}"
	  outputs:
		stdout:
			- "{shared.config.json}"
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 1, result.Passed, "output:\n%s", buf.String())
}

func TestRunFileNonLocalSharedPlaceholderLeftVerbatim(t *testing.T) {
	path := writeRunnerDats(t, `
tests:
	- cmd: echo '{shared.../escape}'
	  outputs:
		stdout:
			- "{shared.../escape}"
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 1, result.Passed, "output:\n%s", buf.String())
}

func TestRunFileSetupRunsBeforeTests(t *testing.T) {
	// Setup output is observable from the tests, proving it ran first.
	path := writeRunnerDats(t, `
setup:
	- echo generated-by-setup > {shared.marker.txt}
tests:
	- cmd: cat {shared.marker.txt}
	  outputs:
		stdout:
			- generated-by-setup
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 1, result.Passed, "output:\n%s", buf.String())
	assert.True(t, result.Ok())
}

func TestRunFileSetupFailureFailsEveryTestAndRunsTeardown(t *testing.T) {
	teardownMarker := filepath.Join(t.TempDir(), "teardown-ran.txt")
	neverMarker := filepath.Join(t.TempDir(), "never.txt")
	path := writeRunnerDats(t, `
setup:
	- echo before-failure
	- exit 3
	- touch `+neverMarker+`
teardown:
	- touch `+teardownMarker+`
tests:
	- desc: first
	  cmd: echo one
	- desc: second
	  cmd: echo two
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)

	// Every test is reported as a failure, never skipped.
	assert.Equal(t, 0, result.Passed)
	assert.Equal(t, 2, result.Failed)
	require.Len(t, result.Results, 2)
	for _, tr := range result.Results {
		assert.False(t, tr.Passed)
		require.Len(t, tr.Failures, 1)
		assert.Contains(t, tr.Failures[0], "file setup failed")
	}
	require.NotNil(t, result.SetupFailure)
	assert.Equal(t, "exit 3", result.SetupFailure.Command)
	assert.Contains(t, result.SetupFailure.Detail, "exit code 3")
	assert.False(t, result.Ok())

	out := buf.String()
	assert.Contains(t, out, "# setup command failed: exit 3")
	assert.Contains(t, out, "not ok 1 - first")
	assert.Contains(t, out, "not ok 2 - second")
	assert.Contains(t, out, "file setup failed")
	assert.NotContains(t, out, "skip")

	// Setup stopped at the first failure...
	_, statErr := os.Stat(neverMarker)
	assert.True(t, os.IsNotExist(statErr), "setup must stop at the first failing command")
	// ...but teardown still ran.
	_, statErr = os.Stat(teardownMarker)
	assert.Nil(t, statErr, "teardown must run even when setup failed")
}

func TestRunFileSetupFailureShowsCapturedOutput(t *testing.T) {
	path := writeRunnerDats(t, `
setup: echo partial output; echo boom >&2; exit 7
tests:
	- cmd: echo never runs
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	require.NotNil(t, result.SetupFailure)
	assert.Equal(t, "partial output\n", result.SetupFailure.Stdout)
	assert.Equal(t, "boom\n", result.SetupFailure.Stderr)

	out := buf.String()
	assert.Contains(t, out, "#   stdout:\n#     partial output\n")
	assert.Contains(t, out, "#   stderr:\n#     boom\n")
}

func TestRunFileSharedWriteFailureFailsEveryTest(t *testing.T) {
	// A shared file whose name collides with a directory created by an
	// earlier shared file cannot be written; the failure is treated exactly
	// like a setup failure: every test fails, teardown still runs.
	marker := filepath.Join(t.TempDir(), "teardown-ran.txt")
	path := writeRunnerDats(t, `
shared:
	files:
		sub/inner.txt: makes sub a directory
		sub: collides with the directory
teardown: touch `+marker+`
tests:
	- desc: never runs
	  cmd: echo hi
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	require.NotNil(t, result.SetupFailure)
	assert.Equal(t, "", result.SetupFailure.Command)
	assert.Contains(t, result.SetupFailure.Detail, "shared fixtures:")
	assert.Equal(t, 1, result.Failed)
	assert.Contains(t, buf.String(), "# setup failed: shared fixtures:")
	assert.Contains(t, buf.String(), "not ok 1 - never runs")

	_, statErr := os.Stat(marker)
	assert.Nil(t, statErr, "teardown must run even when writing shared fixtures failed")
}

func TestRunFileTeardownFailureFailsFile(t *testing.T) {
	path := writeRunnerDats(t, `
teardown: exit 1
tests:
	- cmd: echo ok
	  outputs:
		stdout:
			- ok
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 1, result.Passed)
	assert.Equal(t, 0, result.Failed)
	require.Len(t, result.TeardownFailures, 1)
	assert.Equal(t, "exit 1", result.TeardownFailures[0].Command)
	assert.Contains(t, result.TeardownFailures[0].Detail, "exit code 1")
	assert.False(t, result.Ok(), "a teardown failure must fail the file even when all tests passed")

	out := buf.String()
	assert.Contains(t, out, "ok 1 - echo ok")
	assert.Contains(t, out, "# teardown command failed: exit 1")
	assert.Contains(t, out, "1/1 passed, teardown failed")
}

func TestRunFileTeardownRunsAfterTestFailuresAndContinuesPastFailures(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.txt")
	last := filepath.Join(dir, "last.txt")
	path := writeRunnerDats(t, `
teardown:
	- touch `+first+`
	- exit 7
	- touch `+last+`
tests:
	- cmd: exit 5
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 1, result.Failed) // the test itself failed
	require.Len(t, result.TeardownFailures, 1)
	assert.Contains(t, result.TeardownFailures[0].Detail, "exit code 7")

	// Teardown ran after the test failure, and one failing teardown command
	// did not stop the rest.
	_, err1 := os.Stat(first)
	assert.Nil(t, err1, "first teardown command must run after test failures")
	_, err2 := os.Stat(last)
	assert.Nil(t, err2, "teardown commands after a failing one must still run")
}

func TestRunFileSharedNestedNamesAndContentExpansion(t *testing.T) {
	path := writeRunnerDats(t, `
shared:
	files:
		sub/dir/base.txt: base-content
		pointer.txt: "{shared.sub/dir/base.txt}"
tests:
	- desc: nested file exists with parents created
	  cmd: cat {shared.sub/dir/base.txt}
	  outputs:
		stdout:
			- base-content
	- desc: shared contents expand shared placeholders
	  cmd: cat "$(cat {shared.pointer.txt})"
	  outputs:
		stdout:
			- base-content
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.Equal(t, 2, result.Passed, "output:\n%s", buf.String())
}

func TestRunFileHooksReceiveCoverDir(t *testing.T) {
	// Under --coverdir, file-level setup and teardown commands receive
	// GOCOVERDIR exactly like test commands do (one shared env-construction
	// path), so coverage captures every invocation of an instrumented binary.
	dir := t.TempDir()
	setupMarker := filepath.Join(dir, "setup-cover.txt")
	teardownMarker := filepath.Join(dir, "teardown-cover.txt")
	path := writeRunnerDats(t, `
setup: echo "$GOCOVERDIR" > `+setupMarker+`
teardown: echo "$GOCOVERDIR" > `+teardownMarker+`
tests:
	- cmd: echo hi
`)
	coverDir := filepath.Join(t.TempDir(), "coverage")
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, coverDir)
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.True(t, result.Ok(), "output:\n%s", buf.String())

	setupSeen, err := os.ReadFile(setupMarker)
	require.Nil(t, err)
	assert.Equal(t, coverDir+"\n", string(setupSeen), "setup must observe GOCOVERDIR")
	teardownSeen, err := os.ReadFile(teardownMarker)
	require.Nil(t, err)
	assert.Equal(t, coverDir+"\n", string(teardownSeen), "teardown must observe GOCOVERDIR")
}

func TestRunFileHooksWithoutCoverDirInheritPlainEnv(t *testing.T) {
	// Without --coverdir, hooks run with plain inheritance: they see the
	// parent's own GOCOVERDIR exactly as-is and are never handed a value
	// they didn't inherit.
	t.Setenv("GOCOVERDIR", "from-parent")
	marker := filepath.Join(t.TempDir(), "cover.txt")
	path := writeRunnerDats(t, `
setup: echo "cover=$GOCOVERDIR" > `+marker+`
tests:
	- cmd: echo hi
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.True(t, result.Ok(), "output:\n%s", buf.String())

	seen, err := os.ReadFile(marker)
	require.Nil(t, err)
	assert.Equal(t, "cover=from-parent\n", string(seen))
}

func TestRunFileVerboseShowsHookCommands(t *testing.T) {
	path := writeRunnerDats(t, `
setup: echo prepare
teardown: echo cleanup
tests:
	- cmd: echo hi
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, true, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.True(t, result.Ok())
	assert.Contains(t, buf.String(), "# setup: echo prepare")
	assert.Contains(t, buf.String(), "# teardown: echo cleanup")
}

func TestRunFileHookEnvApplied(t *testing.T) {
	// FOO is a literal value and BAR expands {shared.X}, proving both the env
	// values themselves and their placeholder expansion reach the command.
	path := writeRunnerDats(t, `
shared:
	files:
		marker.txt: from-shared
setup:
	- cmd: echo "$FOO/$BAR" > {shared.out.txt}
	  env:
		FOO: bar
		BAR: "{shared.marker.txt}"
tests:
	- cmd: cat {shared.out.txt}
	  outputs:
		stdout:
			- bar/
			- /shared/marker.txt
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.True(t, result.Ok(), "output:\n%s", buf.String())
}

func TestRunFileHookEnvNotInheritedByTests(t *testing.T) {
	// A hook's env is scoped to that hook command; it must not leak into the
	// test commands that run afterward.
	path := writeRunnerDats(t, `
setup:
	- cmd: "true"
	  env:
		FOO: bar
tests:
	- cmd: echo "[$FOO]"
	  outputs:
		stdout:
			- "[]"
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.True(t, result.Ok(), "output:\n%s", buf.String())
}

func TestRunFileHookStdinFile(t *testing.T) {
	dir := t.TempDir()
	require.Nil(t, os.WriteFile(filepath.Join(dir, "in.txt"), []byte("piped content"), 0644))
	path := filepath.Join(dir, "runner.dats")
	require.Nil(t, os.WriteFile(path, []byte(`
setup:
	- cmd: cat > {shared.out.txt}
	  stdin_file: in.txt
tests:
	- cmd: cat {shared.out.txt}
	  outputs:
		stdout:
			- piped content
`), 0644))
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.True(t, result.Ok(), "output:\n%s", buf.String())
}

func TestRunFileHookStdinFileMissingFailsLoudly(t *testing.T) {
	path := writeRunnerDats(t, `
setup:
	- cmd: cat
	  stdin_file: does-not-exist.txt
tests:
	- cmd: echo never
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	require.NotNil(t, result.SetupFailure)
	assert.Contains(t, result.SetupFailure.Detail, "reading stdin_file")
}

func TestRunFileHookTimeoutEnforced(t *testing.T) {
	path := writeRunnerDats(t, `
setup:
	- cmd: sleep 5
	  timeout: 200ms
tests:
	- cmd: echo never
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	start := time.Now()
	result, err := r.RunFile(context.Background(), path)
	elapsed := time.Since(start)
	require.Nil(t, err)
	assert.Less(t, elapsed, 3*time.Second, "the hook's own timeout must cut the sleep short")
	require.NotNil(t, result.SetupFailure)
	assert.Contains(t, result.SetupFailure.Detail, "command timed out after 200ms")
}

func TestRunFileHookDefaultTimeoutDoesNotFireEarly(t *testing.T) {
	// A hook with no stated timeout runs under DefaultHookTimeout (30s), not
	// some shorter implicit bound -- a quick command must not be cut off.
	path := writeRunnerDats(t, `
setup: echo quick
tests:
	- cmd: echo hi
`)
	var buf bytes.Buffer
	r := NewRunner(&buf, false, false, "")
	result, err := r.RunFile(context.Background(), path)
	require.Nil(t, err)
	assert.True(t, result.Ok(), "output:\n%s", buf.String())
}

func TestRunFileCanceledContextTeardownStillRuns(t *testing.T) {
	// Canceling the context mid-run aborts the in-flight test command
	// promptly and reports the instance as failed -- but teardown runs under
	// context.WithoutCancel, so the cleanup marker must still appear.
	marker := filepath.Join(t.TempDir(), "teardown-ran")
	path := writeRunnerDats(t, `
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
	result, err := r.RunFile(ctx, path)
	elapsed := time.Since(start)
	require.Nil(t, err)
	assert.Less(t, elapsed, 2*time.Second, "cancellation must abort the run promptly")
	assert.Equal(t, 1, result.Failed)
	assert.False(t, result.Ok())
	assert.FileExists(t, marker, "teardown must run under context.WithoutCancel even after cancellation")
}
