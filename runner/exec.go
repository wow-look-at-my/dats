package runner

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"
)

// ExecResult contains the result of executing a command
type ExecResult struct {
	ExitCode    int
	Stdout      string
	StdoutLines []string
	Stderr      string
	StderrLines []string
	TimedOut    bool   // true if the command was killed because timeout elapsed
	Signal      string // signal that terminated the command (e.g. "killed"), or "" if none
}

// waitDelay bounds how long a finished (or killed) command's stray
// descendants may keep the stdout/stderr pipes open before the pipes are
// forcibly closed. It applies to every run, so an orphaned background child
// can never block a test indefinitely.
const waitDelay = 1 * time.Second

// Execute runs a command and captures its output. If timeout > 0, the
// command's whole process group is killed when the deadline elapses and the
// result's TimedOut flag is set.
func Execute(cmd string, stdin string, env []string, timeout time.Duration) (*ExecResult, error) {
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Use bash -c to run the command
	command := exec.CommandContext(ctx, "bash", "-c", cmd)

	var stdoutBuf, stderrBuf bytes.Buffer
	command.Stdout = &stdoutBuf
	command.Stderr = &stderrBuf

	if stdin != "" {
		command.Stdin = strings.NewReader(stdin)
	}

	if len(env) > 0 {
		command.Env = env
	}

	// Run the child in its own process group (where supported) so a timeout
	// can kill the whole tree, not just the direct bash process.
	setProcAttrs(command)

	// On timeout, kill the entire process group: the default Cancel kills
	// only the direct child, leaving grandchildren running (and holding the
	// output pipes open).
	var cancelFired atomic.Bool
	command.Cancel = func() error {
		cancelFired.Store(true)
		if err := killProcessGroup(command.Process); err != nil {
			return command.Process.Kill()
		}
		return nil
	}

	// Once the command itself has exited (or the deadline has passed), give
	// lingering pipe holders (orphaned grandchildren) waitDelay to let go,
	// then force the pipes closed instead of blocking forever.
	command.WaitDelay = waitDelay

	err := command.Run()

	state := command.ProcessState
	if state == nil {
		// The command never started (e.g. bash could not be spawned).
		return nil, err
	}

	// From here on the process state is authoritative. err may additionally
	// be exec.ErrWaitDelay (the command exited successfully but a stray
	// descendant held its pipes open past waitDelay -- not a failure), an
	// *exec.ExitError (non-zero exit or signal death, both captured in
	// state), or a context error (the deadline fired but the command had
	// already completed successfully).
	result := &ExecResult{
		ExitCode:    state.ExitCode(), // -1 when terminated by a signal
		Stdout:      stdoutBuf.String(),
		Stderr:      stderrBuf.String(),
		StdoutLines: splitLines(stdoutBuf.String()),
		StderrLines: splitLines(stderrBuf.String()),
		Signal:      stateSignal(state),
	}

	// A run counts as timed out only when the deadline expired, our cancel
	// actually fired, and the command did not complete on its own: a command
	// that exits -- even exactly at the deadline -- is reported by its exit
	// code, never as a timeout.
	result.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded) &&
		cancelFired.Load() &&
		!state.Success() &&
		(result.Signal != "" || !signalsSupported)

	return result, nil
}

// splitLines splits a string into lines, handling the trailing newline properly
func splitLines(s string) []string {
	if s == "" {
		return []string{}
	}
	// Remove trailing newline before splitting to avoid empty last element
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}
