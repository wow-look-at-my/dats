# The `wow-look-at-my/dats` composite action

`action.yml` at the repo root downloads the newest dats from buildhost and runs
it, so a consumer's workflow does not hand-roll a curl + chmod. This holds the
prose that used to sit in `#` blocks inside it; the guard that moved it here is
`wow-look-at-my/actions@yaml-comment-block`, which allows one comment line at a
time.

## Platform defaults

`os`/`arch` default to the runner's own platform: `buildhost-download` reads
`RUNNER_OS`/`RUNNER_ARCH` when neither is given.

## Installing bubblewrap

dats sandboxes test commands by default (bubblewrap on Linux, seatbelt on
macOS, docker otherwise) — real isolation, not a formality, so a caller should
never have to reach for `--no-sandbox` just to work around infrastructure. The
install step skips on non-Linux; macOS's seatbelt backend needs nothing
installed.

## Clearing the user-namespace restriction, and why the action no longer judges the result

bwrap's user namespace can be blocked two different ways. Ubuntu 24.04's
AppArmor restriction (`kernel.apparmor_restrict_unprivileged_userns`) is a real
kernel sysctl a workflow step can clear — confirmed on this repo's own
GitHub-hosted runner. The org's self-hosted `wow-linux` fleet has neither that
sysctl nor the older `kernel.unprivileged_userns_clone` (both report "no such
file"); there the user namespace comes from the hook's `seccomp.userns` opt-in,
which is deployment state, not something a step can set.

So the step clears what is clearable and stops there. **It deliberately does
not decide whether a sandbox is possible.** It used to, by running a bwrap
command of its own and failing the job when that command failed — and that
hand-rolled command was a THIRD copy of dats' isolation flags, after
`runner/sandbox.go` and the smoke test in the webhooks repo. Copies of those
flags have drifted before, in exactly the way that matters: when dats was
missing `--unshare-user`, so was every copy, so nothing caught it
(`wow-look-at-my/dats#41`).

The copy here was worse than redundant. It asked for `--proc /proc`, which a
container refuses whenever its `/proc` is masked
([docs/sandbox-masked-proc.md](sandbox-masked-proc.md)) — so on exactly the
runners this action exists to serve, it would report "no usable sandbox
backend" and fail the job for a sandbox dats can build perfectly well.

dats is the authority on whether dats can sandbox. It probes its own backends,
in its own order, and fails closed with an actionable message naming the
opt-out. A pre-flight guess can only agree with that answer or be wrong about
it.
