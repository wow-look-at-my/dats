//go:build unix && !linux

package runner

import (
	"os/exec"
	"syscall"
)

// A nice value here belongs to the whole process, so the parent cannot lower
// only the thread that forks.
func startCommand(cmd *exec.Cmd, lowPriority bool) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	if lowPriority {
		_ = syscall.Setpriority(syscall.PRIO_PGRP, cmd.Process.Pid, 19)
	}
	return nil
}
