package runner

// How a bwrap sandbox gets its /proc, and the gate on the weaker shape.
//
// A container runtime obscures parts of /proc, and the kernel then refuses the
// sandbox a private procfs. The fallback binds the existing one read-only, which
// keeps every containment property and gives up exactly one: a command can see
// the container process list. That single cost is why the shape is gated rather
// than simply taken -- see procBindWouldRevealOutsideProcesses.
//
// Depth, including the measured property table: docs/sandbox-masked-proc.md.

import (
	"fmt"
	"os"
	"strings"
)

// procMode is how a bwrap sandbox gets its /proc. The zero value is the one
// dats asks for first; procShared is the fallback the probe falls back TO, and
// it is only ever reached because the kernel refused the other one.
type procMode int

const (
	// procFresh mounts a private procfs for the sandbox's own PID namespace.
	procFresh procMode = iota
	// procShared binds the procfs this process already has, read-only,
	// because a fresh one cannot be mounted here. See procSharedReason.
	procShared
)

// procSharedReason is what the fallback costs, stated where it is chosen.
//
// A container runtime masks parts of /proc: docker binds /dev/null over
// /proc/kcore and friends, mounts a size-0 tmpfs over /proc/acpi and friends,
// and read-only binds /proc/sys and /proc/sysrq-trigger. Those mounts become
// MNT_LOCKED the instant bwrap unshares a user namespace, and the kernel
// refuses a fresh procfs while any locked mount obscures the procfs already
// visible there -- mount_too_revealing in fs/namespace.c, reported by bwrap as
// "Can't mount proc on /newroot/proc: Operation not permitted". Nothing inside
// the container can clear those mounts: unmounting them needs CAP_SYS_ADMIN in
// the init user namespace, which is exactly what a container does not have.
//
// So the choice in a masked container is this fallback or no sandbox at all.
// Measured, both shapes, against docker's own mask set:
//
//	property                          fresh   shared
//	its own PID namespace             yes     yes
//	can signal processes outside      no      no
//	host filesystem writable          no      no
//	/proc/self/exe, /proc/self/fd     yes     yes
//	hides the other processes         yes     NO
//
// The sandbox keeps every containment property and loses concealment: a
// command can SEE the process list of the container dats is running in, and
// cannot touch it. Nothing about the container's own confinement changes --
// this asks the kernel for strictly less, never for more.
const procSharedReason = "a fresh procfs is refused here (the container's /proc is masked), " +
	"so the sandbox binds the existing /proc read-only: it keeps its own PID namespace and " +
	"cannot signal or write outside, but CAN see this container's process list"

// initialPIDNamespace is the kernel's fixed inode for the machine's own PID
// namespace: PROC_PID_INIT_INO, 0xEFFFFFFC, in include/linux/proc_ns.h. Every
// namespace created afterwards gets an ordinary allocated inode.
const initialPIDNamespace = "pid:[4026531836]"

// procBindWouldRevealOutsideProcesses names the reason the shared-/proc shape
// is unsafe here, or "" when it is safe. It is the gate on the ONE property
// that fallback gives up, and it is checked, never assumed.
//
// The fallback binds the procfs dats can already see. Under a container runtime
// that procfs belongs to the container's own PID namespace, so it holds the
// container's processes and nothing else -- the runtime established that
// namespace long before bwrap ran, and a bind cannot reveal what the mount
// never contained. Two other arrangements are NOT safe, and a masked /proc is
// no evidence against either:
//
//   - The machine's own PID namespace. The bind hands a test command the whole
//     host process table. Read from the namespace inode: the kernel gives the
//     initial one a fixed value, and every later namespace an allocated one.
//   - A PID namespace of our own, with an ANCESTOR's procfs still mounted.
//     The namespace inode looks fine and /proc still lists the outside. procfs
//     renders NSpid relative to the READING mount, so it holds one pid when
//     the mount is our own namespace's and the whole chain when it is not --
//     measured: "NSpid: 3" inside `unshare --pid --fork --mount-proc`, and
//     "NSpid: 14494 3" for the same process without --mount-proc.
//
// Fails closed: a /proc that cannot be read proves nothing about its scope, so
// it is refused rather than trusted.
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

// nspidDepth counts the pids on procfs' NSpid line, which is one per PID
// namespace between the reading mount's and the process's own. It returns 0
// when the field is absent, which no supported kernel does and which must not
// read as the safe answer.
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
