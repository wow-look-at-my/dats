//go:build linux

package runner

import (
	"os/exec"
	"runtime"
	"syscall"
)

// On Linux a nice value belongs to a thread, and a child takes it from the
// thread that forks it. That thread must live until the command exits: the
// parent-death signal that bwrap --die-with-parent sets fires when the forking
// thread exits.
func startCommand(cmd *exec.Cmd, lowPriority bool) (release func(), err error) {
	if !lowPriority {
		return func() {}, cmd.Start()
	}
	started := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		// The thread stays locked, so the runtime ends it with this goroutine.
		runtime.LockOSThread()
		// A cosmo binary on a Windows host has no setpriority, and the command still runs.
		_ = syscall.Setpriority(syscall.PRIO_PROCESS, syscall.Gettid(), 19)
		if err := cmd.Start(); err != nil {
			started <- err
			return
		}
		started <- nil
		<-done
	}()
	if err := <-started; err != nil {
		return nil, err
	}
	return func() { close(done) }, nil
}
