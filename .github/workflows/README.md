# Why the CI workflow is shaped the way it is

The prose that used to sit in `#` blocks inside `ci.yml`. It moved here when
`go-toolchain@v1` began embedding `wow-look-at-my/actions@yaml-comment-block`,
which fails CI on any run of more than one comment line.

Each heading is the anchor a one-line comment in the YAML points at. Keep them
in sync: renaming a heading orphans a pointer.

## test: artifact-metadata and deployments

Publishing records what buildhost stored on the org's linked artifacts page and
registers a GitHub Deployment for the release; without these the publish step
fails (HTTP 404 / "Resource not accessible by integration"). Job-level
permissions REPLACE workflow-level ones, so they belong in the job.

## test: installing bubblewrap and clearing the restriction

The sandbox integration tests skip themselves when no backend is usable, so
this step is what makes them mean something here: the docker backend uses the
runner's own daemon, while bubblewrap has to be installed AND allowed a user
namespace — ubuntu-24.04 denies that by default, which silently turned every
bwrap test into a skip. The probe is the gate: an unusable bwrap fails the
build with its own error, before a single test runs.

The value is logged before it is cleared, so each run records whether the
restriction was actually in force — the reason the clearing line exists at all.
A kernel without the knob prints nothing.

This job runs on a GitHub-hosted VM, whose `/proc` is not masked, so the plain
`--proc /proc` probe here is the right one. That is NOT true inside a
container — see "self-hosted-sandbox-unprivileged" below and
[docs/sandbox-masked-proc.md](../../docs/sandbox-masked-proc.md).

## self-hosted-sandbox

Consumers reference this repo's own `action.yml` (`wow-look-at-my/dats@master`)
to get real sandboxing on the org's self-hosted fleet — a class of runner this
repo's other jobs never touch (they are all `ubuntu-latest`). This uses the
dind pool the way a real consumer does; the container is privileged, so dats'
bwrap → seatbelt → docker order lands on bwrap. It catches a regression in
either the action or a backend before a consumer's CI does.

## self-hosted-sandbox-unprivileged

The SAME suite on the UNPRIVILEGED pool — the job that would have caught every
sandbox bug this repo has shipped.

The job above runs on the dind pool, whose container is `--privileged` and so
holds CAP_SYS_ADMIN. bwrap works there no matter what: it can create the PID
and mount namespaces directly, and it can mount procfs over a masked `/proc`.
That kept it green through TWO broken states — one with no `--unshare-user` at
all, one applying `--proc` before `--unshare-pid` — because neither mistake
costs anything when you already hold the capability. Both were immediately
fatal on `wow-linux`, which is where consumers' CI actually runs.

A sandbox exercised only under privilege is not exercised. This job is the
difference between "dats' CI is green" and "dats sandboxes".

It depends on the fleet, so read a failure carefully before blaming the code:
the slim runner needs its `seccomp.userns` opt-in DEPLOYED to create a user
namespace at all, and while the hooks tree that declares it is held, bwrap is
refused here for a reason no change in this repo can fix.
