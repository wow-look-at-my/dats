//go:build windows

package runner

import (
	"errors"
	"os"
	"os/exec"
)

const signalsSupported = false

// setProcAttrs is a no-op on windows; process-group control is not wired up.
func setProcAttrs(cmd *exec.Cmd) {}

// killProcessGroup always fails on windows so the caller falls back to killing the direct child.
func killProcessGroup(p *os.Process) error {
	return errors.New("process groups are not supported on windows")
}

// stateSignal always reports no signal on windows.
func stateSignal(state *os.ProcessState) string { return "" }

func setLowPriority(pid int) error { return nil }
