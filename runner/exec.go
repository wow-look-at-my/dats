package runner

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// ExecResult contains the result of executing a command
type ExecResult struct {
	ExitCode    int
	Stdout      string
	StdoutLines []string
	Stderr      string
	StderrLines []string
	TimedOut    bool // true if the command was killed because timeout elapsed
}

// Execute runs a command and captures its output. If timeout > 0, the command
// is killed when the deadline elapses and the result's TimedOut flag is set.
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

	err := command.Run()

	result := &ExecResult{
		ExitCode:    0,
		Stdout:      stdoutBuf.String(),
		Stderr:      stderrBuf.String(),
		StdoutLines: splitLines(stdoutBuf.String()),
		StderrLines: splitLines(stderrBuf.String()),
		TimedOut:    timeout > 0 && errors.Is(ctx.Err(), context.DeadlineExceeded),
	}

	// Extract exit code from error
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				result.ExitCode = status.ExitStatus()
			}
		} else if !result.TimedOut {
			// Command failed to start (and not because we killed it on timeout)
			return nil, err
		}
	}

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
