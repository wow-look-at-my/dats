# The masked-/proc fallback

A container runtime masks parts of `/proc`. Inside such a container, the kernel
refuses the bwrap sandbox a private procfs, and dats used to fail the whole run
with `no usable sandbox backend`. It now falls back to a read-only bind of the
container's own `/proc`, keeps every containment property, and says so.

## What the kernel refuses, and why nothing inside the container can fix it

Docker mounts three shapes over `/proc` in every container:

- a size-0 tmpfs over `/proc/acpi`, `/proc/asound`, `/proc/scsi`
- a bind of `/dev/null` over `/proc/kcore`, `/proc/keys`, `/proc/latency_stats`,
  `/proc/timer_list`, `/proc/sched_debug`
- read-only self-binds of `/proc/bus`, `/proc/fs`, `/proc/irq`, `/proc/sys`,
  `/proc/sysrq-trigger`

When bwrap unshares a user namespace, every mount copied into the new mount
namespace becomes `MNT_LOCKED` — the sandbox must not be able to reveal what
they hide by unmounting them. `mount_too_revealing()` in `fs/namespace.c` then
refuses a fresh procfs unless some procfs mount in that namespace is fully
visible, and each of those three shapes on its own is enough to disqualify the
container's `/proc`. bwrap reports the refusal as:

```
bwrap: Can't mount proc on /newroot/proc: Operation not permitted
```

Measured against docker's own mask set: with no `/proc` submounts a suite
passes; with the tmpfs alone, the `/dev/null` bind alone, or the read-only bind
alone, that error reproduces exactly.

Nothing in the container can clear those mounts. Unmounting them needs
`CAP_SYS_ADMIN` in the initial user namespace, which is exactly what a
container does not hold — and granting it, or clearing the masks with
`--security-opt systempaths=unconfined`, hands the container's root a writable
`/proc/sysrq-trigger`, i.e. a one-line host reboot. The sandbox must not cost
the host that.

### `subset=pid` does not help

A procfs mounted `-o subset=pid` is a "restricted variant", and current
mainline exempts restricted variants from the visibility rule entirely. That
exemption is not in any shipping kernel yet: `v6.18`'s `mount_too_revealing()`
goes straight to `!mnt_already_visible(...)` with no such branch, and measured
on 6.18 both a plain procfs and `-o subset=pid` are refused under masking. It
would also hide `/proc/cpuinfo`, `/proc/meminfo`, `/proc/stat` and
`/proc/mounts`, which real test commands read.

## What the fallback keeps

`--unshare-pid` stays. The kernel objected to mounting a procfs, never to the
PID namespace, so dropping it would trade away containment to buy back
concealment. Only the `/proc` argument changes: `--proc /proc` becomes
`--ro-bind /proc /proc`.

| property | fresh procfs | shared /proc |
|---|---|---|
| its own PID namespace | yes | yes |
| can signal processes outside | no | no |
| host filesystem writable | no | no |
| `/proc/sysrq-trigger`, `/proc/sys` writable | no | no |
| `/proc/self/exe`, `/proc/self/fd`, bash `<(…)` | yes | yes |
| hides the other processes | yes | **no** |

The one loss is concealment, and it is bounded by the container: what a command
can see is the process list of the container dats is running in, which it
cannot signal or write to. The fallback asks the kernel for strictly less than
the private procfs did — it adds no reach of any kind.

## How it is chosen, and how it is announced

`probeBwrap` asks for the private procfs first and only tries the bind when the
kernel refuses, so a host that can give the stronger sandbox always gets it.
The result is memoized with the backend, one probe per process.

A reduced sandbox is never silent. Every file it applies to is announced:

```
# sandbox: bwrap (shared /proc)
```

and the run explains it once, on the first such file:

```
# sandbox: a fresh procfs is refused here (the container's /proc is masked), so the
  sandbox binds the existing /proc read-only: it keeps its own PID namespace and
  cannot signal or write outside, but CAN see this container's process list
```

## Tests

`runner/sandbox_maskedproc_linux_test.go` proves it against a real refusal
rather than an argv string. It re-execs the test binary under
`unshare --user --map-root-user --mount` (a Go process is multithreaded by the
time a test runs, and `unshare(CLONE_NEWUSER)` is refused to one), obscures a
`/proc` subdirectory with a tmpfs, and then requires — as a negative control —
that a private procfs really is refused before asserting the probe returns the
fallback. Without that control the test would pass just as happily on a host
that never needed one. It needs no privilege and no container.

`TestBwrapSharedProcKeepsTheContainment` pins the argv: the two shapes must
differ in the `/proc` argument and in nothing else.
