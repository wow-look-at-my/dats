//go:build unix

package runner

import (
	"os"
	"os/exec"
	"syscall"
)

const signalsSupported = true

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

func setLowPriority(pid int) error {
	return syscall.Setpriority(syscall.PRIO_PGRP, pid, 19)
}

func stateSignal(state *os.ProcessState) string {
	if ws, ok := state.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return ws.Signal().String()
	}
	return ""
}
