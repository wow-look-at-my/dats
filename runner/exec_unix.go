//go:build unix

package runner

import (
	"os"
	"os/exec"
	"sync"
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

// nicePath resolves nice once per run. Empty when the host has none.
var nicePath = sync.OnceValue(func() string {
	path, err := exec.LookPath("nice")
	if err != nil {
		return ""
	}
	return path
})

// lowPriorityArgv prefixes argv so the child lowers its OWN priority before the
// real command runs. Renicing from the parent after the start loses a race that
// a short command wins, and nice execs in place, so the pid and the process
// group the canceller kills stay the same. Empty means the host has no nice.
func lowPriorityArgv(argv []string) []string {
	path := nicePath()
	if path == "" {
		return nil
	}
	return append([]string{path, "-n", "19"}, argv...)
}

func stateSignal(state *os.ProcessState) string {
	if ws, ok := state.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return ws.Signal().String()
	}
	return ""
}
