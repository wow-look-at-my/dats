# The `wow-look-at-my/dats` composite action

`action.yml` at the repo root downloads the newest dats from buildhost and runs it, so a consumer's workflow does not. This holds the prose that used to sit in `#` blocks inside it. The guard that moved it here is `wow-look-at-my/actions@yaml-comment-block`, which allows one comment line at a time.

## Platform defaults

`os`/`arch` default to the runner's own platform: `buildhost-download` reads `RUNNER_OS`/`RUNNER_ARCH` when neither is given.

## Paths on an NT runner

Every step here is `shell: bash`, and on a Windows runner that bash reads a backslash as an escape and drops. A path interpolated straight into the script — `${{ github.action_path }}` is `D:\a\_actions\...` — therefore arrives as `D:a_actions...`, and the step dies with `No such file or directory`. So the action passes each such path through the step's `env:` and expands it as `${VAR//\\//}`, which converts the separators. The two script steps and the binary's own path all take that route.

The runner half of the same problem is `runner/hostpath.go`: dats converts every path it substitutes into a command. The `every-host` CI job runs this commit's own binary under one script on ubuntu, macos and windows, so that conversion.

## Installing the sandbox backend

dats sandboxes test commands by default (bubblewrap on Linux, seatbelt on macOS, docker otherwise) — real isolation, not a formality, so a caller must never have to reach. `.github/scripts/install-sandbox-backend.sh` owns this, and the action calls that one file.

macOS needs nothing installed: seatbelt is `/usr/bin/sandbox-exec`, shipped with the OS, so the script asserts it is there and exits.

Windows has no native backend and the script says so, leaving dats to probe for docker. A Windows runner's own daemon serves WINDOWS containers, and WSL1 cannot host a Linux one — see [sandbox-internals.md](sandbox-internals.md) for the measurement. A suite on an NT runner needs `--no-sandbox` until that changes.

On Linux the script installs bubblewrap when `bwrap` is not already on PATH. Two things it does that the inline `sudo apt-get` it replaced did not:

- **It only reaches for `sudo` when it is not already root.** A container job usually runs as root with no. When the job is neither root nor able to sudo, it says so and names `--no-sandbox` rather than dying.
- **It tries `apt-get`, then `dnf`, then `apk`,** and names all three when it finds none, instead of assuming every Linux.

It then re-checks that `bwrap` is on PATH and fails there if it is not. Getting that wrong is otherwise reported much later, by dats, as "no usable sandbox backend" — a message about the wrong step.

## Clearing the user-namespace restriction, and why the action no longer judges the result

bwrap's user namespace can be blocked two different ways. Ubuntu 24.04's AppArmor restriction (`kernel.apparmor_restrict_unprivileged_userns`) is a real kernel sysctl a workflow step can clear — confirmed on this repo's own. The org's self-hosted `wow-linux` fleet has neither that sysctl nor the older `kernel.unprivileged_userns_clone` (both report "no such file"). There the user namespace comes from the hook's `seccomp.userns` opt-in, which is deployment state, not something a step can set.

The two sysctls have **opposite polarity**, and the step got this wrong for as long as it existed. `apparmor_restrict_unprivileged_userns` is a restriction: 1 means blocked. So it is cleared to 0. `unprivileged_userns_clone` is a permission: 1 means allowed. A loop that wrote 0 to both switched the second one OFF, denying the namespace the step was called. On an ubuntu-24.04 runner, where both knobs exist and both start at 1, that turned a working host into one. `.github/scripts/allow-unprivileged-userns.sh` now owns both knobs, in the polarity each actually uses, and both the action and this repo's own CI.

So the step clears what is clearable and stops there. **It deliberately does not decide whether a sandbox is possible.**. Copies of those flags have drifted before, in exactly the way that matters: when dats was missing `--unshare-user`.

The copy here was worse than redundant. It asked for `--proc /proc`, which a container refuses whenever its `/proc` is masked ([docs/sandbox-masked-proc.md](sandbox-masked-proc.md)) — so on exactly the runners this.

dats is the authority on whether dats can sandbox. It probes its own backends, in its own order, and fails closed with an actionable message naming the opt-out. A pre-flight guess can only agree with that answer or be wrong about it.

## The input surface is typed, not a raw argv

The action's inputs are `tests`, `working-directory`, `jobs` and `sandbox`. There is deliberately no `args` passthrough: a caller cannot hand dats a raw command line, so nothing a caller types. Each input selects a documented behaviour and is checked against what dats accepts, so a typo fails in the action. The argv is built and sanitized in `.github/scripts/run-dats.ts` (a `wow-look-at-my/actions@typescript` script), which:

- splits `tests` on whitespace and rejects any entry that starts with `-`, is an absolute path, or contains a `..`.
- expands a directory entry to its top-level `*.dats` files (never recursive, never a hidden file).
- rejects a `jobs` that is not a positive integer. A `sandbox` must be `auto`, `bwrap`, `seatbelt`, `docker` or `none`.
- runs `dats -v [-jN] [--sandbox=MODE] test <files...>` from `working-directory` with the downloaded binary, passing each file as its own argument.

`jobs: '1'` is what a stateful suite needs: one command at a time, instances in declaration order.

`sandbox: none` is the opt-out. And it is the RUN-STARTER's decision, never the file's — a `.dats` file still cannot turn its own sandbox off. It is for a suite whose commands must reach the host: one driving the runner's own Docker daemon has nothing. The backend install steps are skipped with it, since no backend is going to be used. On NT that skips building a WSL one.

`-v` is always on, and there is no input to turn it off. Without it a failing leg reports only that a test failed. With it the leg names the test and prints its output. An input will make that a thing a caller remembers to set after the run that needed it.

## The action runs no org-wide repository checks

`common-checks` lints the calling repository. A consumer runs this action to run a test suite, and nothing else.

Running those checks from here pointed them at the consumer. That failed the actions repo on its own linter fixtures. It also failed every Windows job, because one check needs a `yq` that a Windows runner does not carry.

dats gets the same checks from `go-toolchain` in its own CI. Nothing here goes unchecked.

## NT gets its backend from WSL

Windows hosts no sandbox backend of its own. bwrap is Linux, seatbelt is macOS, and the runner's docker daemon serves.

WSL is the Linux an NT runner does have. `install-wsl-backend.sh` registers a distribution, installs bubblewrap into it, and exercises bwrap before the run. The download is a fat APE. So the same file runs its Linux payload inside that distribution: dats then sees an ordinary Linux host. `run-dats.ts` translates the working directory and the binary path with `wslpath` and starts the run with `wsl --cd`.

The distribution arrives as a pinned rootfs that the script downloads and `wsl --import`s. `wsl --install` goes through the Microsoft Store, which hangs unpredictably on a hosted runner, and a gate that flakes is one. Every `wsl.exe` call is bounded and reads empty stdin. So a stall names the step it stopped instead of running the job out of time in silence.

`DATS_WSL_DISTRO` names the distribution, `DATS_WSL_ROOTFS_URL` the rootfs it is imported from, and `DATS_WSL_TIMEOUT_SECONDS` how long the suite itself may run.

## Starting the binary differs per host

The download is a fat APE, so a raw `execve` of it succeeds only on Linux. Darwin refuses the file with `ENOEXEC`: a shell must read the header and exec the payload, which `bash -c '"$0" "$@"'` does while. NT finds an executable by its extension, so the action copies the download to an `.exe` name and starts that. `action-every-host` in this repo's CI is what proves each form, because the neighbouring `every-host` job starts the binary from bash.

Because the sandbox is the point of the action, there is no way to turn it off through the action. A caller that genuinely needs host execution runs the downloaded binary itself (the `path` output) rather than through this action.
