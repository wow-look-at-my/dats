package cmd

import (
	"sync"
	"sync/atomic"
	"testing"
)

var (
	// Held for the life of a test that drives rootCmd.
	rootCmdState sync.Mutex
	// The test holding it, so a repeat call returns instead of waiting on itself.
	rootCmdOwner atomic.Pointer[testing.T]
)

// holdRootCmd serializes the tests that set rootCmd's writers, args or flag
// values. The suite is parallel and the command tree is shared.
func holdRootCmd(t *testing.T) {
	t.Helper()
	if rootCmdOwner.Load() == t {
		return
	}
	rootCmdState.Lock()
	rootCmdOwner.Store(t)
	t.Cleanup(func() {
		rootCmdOwner.Store(nil)
		rootCmdState.Unlock()
	})
}
