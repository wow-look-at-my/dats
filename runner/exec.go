package runner

import (
	"bytes"
	"os/exec"
	"strings"
	"syscall"
)

// ExecResult contains the result of executing a command
type ExecResult struct {
	ExitCode    int
	Stdout      string
	StdoutLines []string
	Stderr      string
	StderrLines []string
}

// Execute runs a command and captures its output
func Execute(cmd string, stdin string, env []string) (*ExecResult, error) {
	// Use bash -c to run the command
	command := exec.Command("bash", "-c", cmd)

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
	}

	// Extract exit code from error
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				result.ExitCode = status.ExitStatus()
			}
		} else {
			// Command failed to start
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
