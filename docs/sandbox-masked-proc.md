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
| reads another process's `environ`, `mem`, `maps`, `cwd`, fd targets | no | no |

The one loss is concealment, in ONE direction: a command can see the other
processes of the container dats runs in. It cannot be seen by them, and the
container's `/proc` never contained the host's processes to begin with — docker
gave the container its own PID namespace long before bwrap ran.

## The host's process table is never in scope, and that is CHECKED

The paragraph above is a property of the container, not of dats, so dats does
not take it on trust. A masked `/proc` says the kernel refused a private
procfs; it says nothing about whose processes the existing one lists. Before
the fallback is taken, `procBindWouldRevealOutsideProcesses` proves the procfs
is scoped, and refuses the shape outright when it cannot. Two arrangements are
refused:

| arrangement | detected by | why it is refused |
|---|---|---|
| the machine's own PID namespace | `/proc/self/ns/pid` is the kernel's fixed `PROC_PID_INIT_INO` (`pid:[4026531836]`) | the bind hands a test command the whole host process table |
| our own PID namespace, an ANCESTOR's procfs mounted over it | `NSpid` in `/proc/self/status` holds more than one pid | the namespace inode looks fine while `/proc` still lists the outside |

`NSpid` works because procfs renders it relative to the READING mount: one pid
when the mount belongs to our own namespace, the whole chain when it does not.
Measured on the same process, `unshare --pid --fork` with `--mount-proc` gives
`NSpid: 3` and without it `NSpid: 14494 3`.

It fails closed. A `/proc` that cannot be read proves nothing about its scope,
so it is refused rather than assumed safe, and the run reports no sandbox
instead of a weaker one. Refusing costs a sandbox; guessing costs the host.

`runner/sandbox_procgate_linux_test.go` drives both refusals — the second by
re-execing under `unshare --pid --fork` with `--mount-proc` deliberately
omitted — and `sandbox_maskedproc_linux_test.go` holds a process outside the
namespace and requires the sandbox not to see it.

What "see" covers is only `cmdline`, `comm` and `status`. Everything gated on
ptrace access stays denied, measured: `environ`, `mem`, `maps`, `stack`, `io`
and `cwd` all return EPERM, and `/proc/<pid>/fd` lists its numbers while every
link reads back empty. `--unshare-user` is what does it — the command holds
capabilities only in its own user namespace, and the check wants them in the
TARGET's. So a sibling's secrets are not reachable, which is the question this
table exists to answer.

Signalling is refused by the PID namespace, separately: the pids it reads are
the container's, and they name nothing in the namespace it can signal into.

The fallback asks the kernel for strictly less than the private procfs did — it
adds no reach of any kind.

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
