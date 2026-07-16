//go:build unix

package runner

import (
	"os"
	"os/exec"
	"syscall"
)

// signalsSupported reports whether a signal-terminated process state is
// distinguishable from a normal exit on this platform.
const signalsSupported = true

// setProcAttrs places the child in its own process group so the whole group
// (including grandchildren) can be signaled at once.
func setProcAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup kills the entire process group of p.
func killProcessGroup(p *os.Process) error {
	pgid, err := syscall.Getpgid(p.Pid)
	if err != nil {
		return err
	}
	return syscall.Kill(-pgid, syscall.SIGKILL)
}

// stateSignal returns the name of the signal that terminated the process
// (e.g. "killed"), or "" when it exited normally.
func stateSignal(state *os.ProcessState) string {
	if ws, ok := state.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return ws.Signal().String()
	}
	return ""
}
