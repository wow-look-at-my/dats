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
container — see "sandbox-unprivileged-masked" below and
[docs/sandbox-masked-proc.md](../../docs/sandbox-masked-proc.md).

## self-hosted-sandbox

Consumers reference this repo's own `action.yml` (`wow-look-at-my/dats@master`)
to get real sandboxing on the org's self-hosted fleet — a class of runner this
repo's other jobs never touch (they are all `ubuntu-latest`). This uses the
dind pool the way a real consumer does; the container is privileged, so dats'
bwrap → seatbelt → docker order lands on bwrap. It catches a regression in
either the action or a backend before a consumer's CI does.

## sandbox-unprivileged-masked

The SAME suite, unprivileged, with a masked `/proc` — the job that would have
caught every sandbox bug this repo has shipped. It builds that container itself
(`.github/scripts/masked-container-sandbox.sh`) rather than borrowing the org's
slim fleet, and it runs the binary THIS commit built.

### Why it stopped using the fleet

It was `runs-on: ${{ vars.CI_RUNNER }}`, and that made a merge gate depend on
another repository's DEPLOYED state. That is not hypothetical: the slim runners
get their user namespace from the gha-runner hook's `seccomp.userns` opt-in,
that opt-in sat undeployed behind a held reload gate, and bwrap was refused
there before dats reached a sandbox — so no dats change could pass. dats' own
pull requests were blocked on a webhooks deploy that was itself waiting on a
dats release, a ring with no opening inside any repo.

Ownership now sits where each half can be acted on. **dats proves the
mechanism**, here, in a container it builds. **webhooks proves the image** — its
gha-runner `dats-smoke.test.ts` runs this same binary on the real slim image and
asserts the same isolation, which is the right place for it: that repo owns the
image and can fix it.

### What keeps it honest

Relaxing seccomp and AppArmor is what lets an unprivileged process make a user
namespace and mount inside it — the axis the fleet relaxes with `seccomp.userns`.
It is NOT `systempaths=unconfined`, one word away and opposite in consequence:
that unmasks `/proc` and gives container root a writable `/proc/sysrq-trigger`,
i.e. a host reboot. Nothing here grants it.

Two assertions stop the job drifting into an easier test:

- a negative control requiring `/proc/sysrq-trigger` to be **non-writable**, so
  the container is provably still the masked case;
- a check that dats reported `bwrap (shared /proc)`, so a pass cannot come from
  a private procfs the mask would have prevented — the fallback under test has
  to have actually run.

### The two user-namespace sysctls point opposite ways

Whether an unprivileged process may make a user namespace is a HOST setting, and
a container inherits the host's answer — so this job has to clear it outside the
container before it starts one. Two knobs govern it and they read alike while
meaning the reverse of each other: `apparmor_restrict_unprivileged_userns` is a
restriction (1 = blocked, clear to 0), `unprivileged_userns_clone` is a
permission (1 = allowed). Writing 0 to both denies the namespace, which is
exactly what this job did on its first run.
`.github/scripts/allow-unprivileged-userns.sh` owns both, and `action.yml` calls
the same file, so the polarity is written down once.

### Why unprivileged at all

`self-hosted-sandbox` runs on the dind pool, whose container is `--privileged`
and so holds CAP_SYS_ADMIN. bwrap works there no matter what: it can create the
PID and mount namespaces directly, and it can mount procfs over a masked
`/proc`. That kept it green through TWO broken states — one with no
`--unshare-user` at all, one applying `--proc` before `--unshare-pid` — because
neither mistake costs anything when you already hold the capability. Both were
immediately fatal unprivileged, which is where consumers' CI actually runs.

A sandbox exercised only under privilege is not exercised.

### It runs THIS commit's binary, not the published one

`uses: ./` downloads the newest dats from buildhost — the binary on the DEFAULT
BRANCH. On a pull request that is master's dats, so this job said nothing
whatsoever about the change under review. Every line of the sandbox could be
rewritten and the job would pass or fail on a binary without the rewrite; the
masked-`/proc` fallback shipped exactly that way.

So it restores the `test` job's `go-build` hand-off and runs `build/dats`, with
`./build/dats --version` first — a missing or unusable hand-off must fail there,
naming itself, rather than surfacing later as a sandbox error.
