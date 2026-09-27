//go:build linux

package runner

import (
	"os/exec"
	"runtime"
	"syscall"
)

// On Linux a nice value belongs to a thread, and a child takes it from the
// thread that forks it.
func startCommand(cmd *exec.Cmd, lowPriority bool) error {
	if !lowPriority {
		return cmd.Start()
	}
	started := make(chan error, 1)
	go func() {
		// The thread stays locked, so the runtime ends it with this goroutine.
		runtime.LockOSThread()
		if err := syscall.Setpriority(syscall.PRIO_PROCESS, syscall.Gettid(), 19); err != nil {
			started <- err
			return
		}
		started <- cmd.Start()
	}()
	return <-started
}
