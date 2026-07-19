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

// setLowPriority moves pid's whole process group to the lowest scheduling
// priority (nice 19). setProcAttrs made the child its own group leader, so
// the group ID is the child's pid: the call renices the child and every
// process it has already forked, and later children inherit the niceness.
// Callers treat this as best-effort -- a child that already exited, or a
// platform refusing the change, is not worth failing a test over.
func setLowPriority(pid int) error {
	return syscall.Setpriority(syscall.PRIO_PGRP, pid, 19)
}

// stateSignal returns the name of the signal that terminated the process
// (e.g. "killed"), or "" when it exited normally.
func stateSignal(state *os.ProcessState) string {
	if ws, ok := state.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return ws.Signal().String()
	}
	return ""
}
