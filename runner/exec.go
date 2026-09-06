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

const waitDelay = 1 * time.Second

// execRequest is a single command to run: the command text plus everything that shapes how it runs.
type execRequest struct {
	Cmd   string
	Stdin string
	// Env is the child's complete environment (nil = inherit ours).
	Env         []string
	EnvExtra    []string
	Timeout     time.Duration
	LowPriority bool
	// Workdir is the absolute directory the command runs in, empty to inherit ours.
	Workdir string
	// Sandbox wraps the command in an OS-level sandbox; nil runs it directly on the host.
	Sandbox *sandboxPlan
}

func Execute(ctx context.Context, cmd string, stdin string, env []string, timeout time.Duration) (*ExecResult, error) {
	return execute(ctx, execRequest{Cmd: cmd, Stdin: stdin, Env: env, Timeout: timeout})
}

// execute runs a single execRequest.
func execute(ctx context.Context, req execRequest) (*ExecResult, error) {
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	// Commands run through bash -c, directly or wrapped by the sandbox backend (which ends in the same bash -c).
	sandboxed := req.Sandbox.command(shellScript(req.Cmd, req.Workdir), req.EnvExtra)
	argv := sandboxed.Argv
	reniceAfterStart := req.LowPriority
	if req.LowPriority {
		if prefixed := lowPriorityArgv(argv); prefixed != nil {
			argv = prefixed
			reniceAfterStart = false
		}
	}
	command := exec.CommandContext(ctx, argv[0], argv[1:]...)

	var stdoutBuf, stderrBuf bytes.Buffer
	command.Stdout = &stdoutBuf
	command.Stderr = &stderrBuf

	if req.Stdin != "" {
		command.Stdin = strings.NewReader(req.Stdin)
	}

	if len(req.Env) > 0 {
		command.Env = req.Env
	}

	setProcAttrs(command)

	var cancelFired atomic.Bool
	command.Cancel = func() error {
		cancelFired.Store(true)
		if sandboxed.Kill != nil {
			go sandboxed.Kill()
		}
		if err := killProcessGroup(command.Process); err != nil {
			return command.Process.Kill()
		}
		return nil
	}

	command.WaitDelay = waitDelay

	if err := command.Start(); err != nil {
		// The command never started (e.g. bash could not be spawned).
		return nil, err
	}

	if reniceAfterStart {
		_ = setLowPriority(command.Process.Pid)
	}

	err := command.Wait()

	state := command.ProcessState
	if state == nil {
		return nil, err
	}

	result := &ExecResult{
		ExitCode:    state.ExitCode(), // negative when terminated by a signal
		Stdout:      stdoutBuf.String(),
		Stderr:      stderrBuf.String(),
		StdoutLines: splitLines(stdoutBuf.String()),
		StderrLines: splitLines(stderrBuf.String()),
		Signal:      stateSignal(state),
	}

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
