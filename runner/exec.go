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

// execRequest is one command to run: the command text plus everything that
// shapes how it runs. It exists so the knobs (priority, sandboxing, the
// environment entries dats itself adds) can grow without every caller and
// every test restating them positionally.
type execRequest struct {
	Cmd   string
	Stdin string
	// Env is the child's complete environment (nil = inherit ours).
	Env []string
	// EnvExtra holds only the entries dats added on top of the parent
	// environment -- a test's inputs.env values and GOCOVERDIR. They are
	// already part of Env; they are listed separately because a container
	// starts from its image's environment and has to be handed them.
	EnvExtra []string
	Timeout  time.Duration
	// LowPriority moves the child's whole process group to the lowest
	// scheduling priority (nice 19 on unix; no-op on windows) right after it
	// starts, so parallel workloads never starve the machine -- or dats
	// itself, which stays at normal priority. Best-effort: renice failures
	// are ignored, and serial runs leave it unset and make no priority
	// syscalls at all.
	LowPriority bool
	// Sandbox wraps the command in an OS-level sandbox; nil runs it directly
	// on the host.
	Sandbox *sandboxPlan
}

// Execute runs a command on the host and captures its output. ctx is the base
// context: canceling it kills the command's whole process group (reported as
// a signal death, never as a timeout). If timeout > 0, the deadline is
// layered on top of ctx and the result's TimedOut flag is set when it
// elapses.
func Execute(ctx context.Context, cmd string, stdin string, env []string, timeout time.Duration) (*ExecResult, error) {
	return execute(ctx, execRequest{Cmd: cmd, Stdin: stdin, Env: env, Timeout: timeout})
}

// execute runs one execRequest.
func execute(ctx context.Context, req execRequest) (*ExecResult, error) {
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	// Commands run through bash -c, directly or wrapped by the sandbox
	// backend (which ends in the same bash -c).
	sandboxed := req.Sandbox.command(req.Cmd, req.EnvExtra)
	command := exec.CommandContext(ctx, sandboxed.Argv[0], sandboxed.Argv[1:]...)

	var stdoutBuf, stderrBuf bytes.Buffer
	command.Stdout = &stdoutBuf
	command.Stderr = &stderrBuf

	if req.Stdin != "" {
		command.Stdin = strings.NewReader(req.Stdin)
	}

	if len(req.Env) > 0 {
		command.Env = req.Env
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
		// A backend whose workload outlives the process we spawned (docker:
		// killing the client leaves the container running) gets torn down
		// too. Asynchronously -- Cancel must not block the kill below behind
		// a round trip to a daemon.
		if sandboxed.Kill != nil {
			go sandboxed.Kill()
		}
		if err := killProcessGroup(command.Process); err != nil {
			return command.Process.Kill()
		}
		return nil
	}

	// Once the command itself has exited (or the deadline has passed), give
	// lingering pipe holders (orphaned grandchildren) waitDelay to let go,
	// then force the pipes closed instead of blocking forever.
	command.WaitDelay = waitDelay

	if err := command.Start(); err != nil {
		// The command never started (e.g. bash could not be spawned).
		return nil, err
	}

	// setProcAttrs put the child in its own process group (where supported),
	// so renicing the group reaches the direct child and everything it has
	// forked; later descendants inherit the niceness from their parent. A
	// command may exit before the call lands -- that race, like any other
	// renice failure, is deliberately ignored.
	if req.LowPriority {
		_ = setLowPriority(command.Process.Pid)
	}

	err := command.Wait()

	state := command.ProcessState
	if state == nil {
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
	// code, never as a timeout. ctx here is the derived per-command context,
	// so a parent cancellation (e.g. Ctrl-C in watch mode) surfaces as
	// context.Canceled, not DeadlineExceeded, and correctly stays TimedOut ==
	// false -- the command shows as a signal death instead.
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
