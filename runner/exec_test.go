package runner

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecute(t *testing.T) {
	result, err := Execute(context.Background(), "echo hello", "", nil, 0)
	require.Nil(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "hello\n", result.Stdout)
	assert.Equal(t, "", result.Stderr)
	assert.Equal(t, []string{"hello"}, result.StdoutLines)
	assert.False(t, result.TimedOut)
}

func TestExecuteWithStdin(t *testing.T) {
	result, err := Execute(context.Background(), "cat", "input text", nil, 0)
	require.Nil(t, err)
	assert.Equal(t, "input text", result.Stdout)
}

func TestExecuteNonZeroExit(t *testing.T) {
	result, err := Execute(context.Background(), "exit 42", "", nil, 0)
	require.Nil(t, err)
	assert.Equal(t, 42, result.ExitCode)
}

func TestExecuteStderr(t *testing.T) {
	result, err := Execute(context.Background(), "echo err >&2", "", nil, 0)
	require.Nil(t, err)
	assert.Equal(t, "err\n", result.Stderr)
	assert.Equal(t, []string{"err"}, result.StderrLines)
}

func TestExecuteWithEnv(t *testing.T) {
	result, err := Execute(context.Background(), "echo $TEST_VAR", "", []string{"TEST_VAR=hello123"}, 0)
	require.Nil(t, err)
	assert.Equal(t, "hello123\n", result.Stdout)
}

func TestExecuteTimeout(t *testing.T) {
	result, err := Execute(context.Background(), "sleep 1", "", nil, 50*time.Millisecond)
	require.Nil(t, err)
	assert.True(t, result.TimedOut)
}

func TestExecuteWithinTimeout(t *testing.T) {
	result, err := Execute(context.Background(), "echo quick", "", nil, 5*time.Second)
	require.Nil(t, err)
	assert.False(t, result.TimedOut)
	assert.Empty(t, result.Signal)
	assert.Equal(t, "quick\n", result.Stdout)
}

func TestExecuteOrphanDoesNotBlock(t *testing.T) {
	// An orphaned background child inherits the stdout pipe; it must not be
	// able to block Execute after bash itself has exited.
	start := time.Now()
	result, err := Execute(context.Background(), "sleep 3 & echo hi", "", nil, 0)
	elapsed := time.Since(start)
	require.Nil(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "hi\n", result.Stdout)
	assert.False(t, result.TimedOut)
	assert.Less(t, elapsed, 2500*time.Millisecond, "orphaned grandchild must not block Execute")
}

func TestExecuteTimeoutKillsOrphans(t *testing.T) {
	// With a timeout, the whole process group is killed, so the orphan cannot
	// hold the pipes open. bash itself exited 0 before the deadline, so the
	// run is a success, not a timeout.
	start := time.Now()
	result, err := Execute(context.Background(), "sleep 3 & echo hi", "", nil, 100*time.Millisecond)
	elapsed := time.Since(start)
	require.Nil(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "hi\n", result.Stdout)
	assert.False(t, result.TimedOut, "bash exited 0; a successful command must never be reported as timed out")
	assert.Less(t, elapsed, 2500*time.Millisecond, "group kill must release the pipes promptly")
}

func TestExecuteTimeoutKillsDirectChild(t *testing.T) {
	start := time.Now()
	result, err := Execute(context.Background(), "sleep 3", "", nil, 100*time.Millisecond)
	elapsed := time.Since(start)
	require.Nil(t, err)
	assert.True(t, result.TimedOut)
	assert.Less(t, elapsed, 2500*time.Millisecond)
}

func TestExecuteParentCancelKillsProcessGroup(t *testing.T) {
	// Canceling the caller's context mid-run kills the whole process group
	// promptly -- including the background child that would otherwise hold
	// the output pipes open -- and reports a signal death, never a timeout
	// (mirrors TestExecuteTimeoutKillsOrphans).
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	result, err := Execute(ctx, "sleep 3 & sleep 3", "", nil, 0)
	elapsed := time.Since(start)
	require.Nil(t, err)
	assert.False(t, result.TimedOut, "parent cancellation must never be reported as a timeout")
	assert.Equal(t, "killed", result.Signal)
	assert.Less(t, elapsed, 2500*time.Millisecond, "group kill must end the run and release the pipes promptly")
}

func TestExecuteParentCancelWithTimeoutNotTimedOut(t *testing.T) {
	// With a timeout layered on top, a parent cancellation that fires first
	// surfaces as context.Canceled on the derived context: TimedOut stays
	// false, distinguishing Ctrl-C from a real deadline.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	result, err := Execute(ctx, "sleep 3", "", nil, 10*time.Second)
	require.Nil(t, err)
	assert.False(t, result.TimedOut)
	assert.Equal(t, "killed", result.Signal)
}

func TestExecuteSignalDeath(t *testing.T) {
	result, err := Execute(context.Background(), "kill -KILL $$", "", nil, 0)
	require.Nil(t, err)
	assert.Equal(t, -1, result.ExitCode)
	assert.Equal(t, "killed", result.Signal)
	assert.False(t, result.TimedOut)
}

func TestExecuteExitCodeSeven(t *testing.T) {
	result, err := Execute(context.Background(), "exit 7", "", nil, 0)
	require.Nil(t, err)
	assert.Equal(t, 7, result.ExitCode)
	assert.Empty(t, result.Signal)
	assert.False(t, result.TimedOut)
}

func TestSplitLines(t *testing.T) {
	assert.Equal(t, []string{}, splitLines(""))
	assert.Equal(t, []string{"a"}, splitLines("a\n"))
	assert.Equal(t, []string{"a", "b"}, splitLines("a\nb\n"))
	assert.Equal(t, []string{"a", "b"}, splitLines("a\nb"))
}
