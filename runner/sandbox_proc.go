package runner

// How a bwrap sandbox gets its /proc, and the gate on the weaker shape.

import (
	"fmt"
	"os"
	"strings"
)

// procMode is how a bwrap sandbox gets its /proc.
type procMode int

const (
	// procFresh mounts a private procfs for the sandbox's own PID namespace.
	procFresh procMode = iota
	procShared
)

// procSharedReason is what the fallback costs, stated where it is chosen.
const procSharedReason = "a fresh procfs is refused here (the container's /proc is masked), " +
	"so the sandbox binds the existing /proc read-only: it keeps its own PID namespace and " +
	"cannot signal or write outside, but CAN see this container's process list"

const initialPIDNamespace = "pid:[4026531836]"

func procBindWouldRevealOutsideProcesses() string {
	ns, err := os.Readlink("/proc/self/ns/pid")
	if err != nil {
		return "cannot read /proc/self/ns/pid, so the scope of this /proc is unprovable"
	}
	if ns == initialPIDNamespace {
		return "this is the machine's own PID namespace, where the bind would show " +
			"a test command every process on the host"
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return "cannot read /proc/self/status, so the scope of this /proc is unprovable"
	}
	if n := nspidDepth(status); n != 1 {
		return fmt.Sprintf("this /proc belongs to an ancestor PID namespace (NSpid depth %d), "+
			"so the bind would show a test command processes outside this container", n)
	}
	return ""
}

func nspidDepth(status []byte) int {
	for _, line := range strings.Split(string(status), "\n") {
		rest, ok := strings.CutPrefix(line, "NSpid:")
		if !ok {
			continue
		}
		return len(strings.Fields(rest))
	}
	return 0
}
