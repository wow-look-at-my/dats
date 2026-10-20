# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

DATS (Declarative Automated Testing System) is a Go CLI that runs tests defined in declarative YAML files (`.dats`). It natively executes commands, captures output, and verifies assertions without requiring external test frameworks.

## Build Commands

```bash
just build          # Build the dats binary to build/dats (runs go fmt, go vet, go build)
just test           # Run Go tests with coverage + run example.dats
just install        # Symlink binary to ~/.local/bin/dats
```

## Running Specific Tests

```bash
# Run only Go unit tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run a .dats test file directly
./build/dats examples/example.dats

# Verbose mode (shows command details, full output on failure)
./build/dats -v examples/example.dats

# Parallel execution (4 workers; bare -j = one per CPU; -j 4 does NOT bind)
./build/dats -j4 examples/example.dats

# Sandboxing is ON by default (auto: bwrap, then seatbelt, then docker). Opt out per run:
./build/dats --no-sandbox examples/example.dats
./build/dats --sandbox=docker --sandbox-image=alpine:3.20 examples/example.dats

# Keep temp directory for debugging
./build/dats --keep-temp examples/example.dats

# Watch mode: run, then re-run the complete scope on file changes (Ctrl-C exits 0)
./build/dats watch examples/
```

## Architecture

[docs/architecture-2.md](docs/architecture-2.md) holds this section.

## DATS File Format

`docs/file-format.md` is the reference. Every key, its accepted forms, its default, and the parse error it raises — ending in a complete field map under "Complete Field Reference". `schema.json` is the machine-readable copy, and `docs/examples.md` holds worked files. The properties an agent needs before opening any of them:

- Indentation is TABS (`docs/file-format.md#yaml-dialect`). A sequence item's sibling keys align with multiple spaces AFTER the tab.
- A file may only NARROW its sandbox. `sandbox: false` and `ssh: false` are parse errors naming `--no-sandbox`: turning isolation off, or pulling a remote command back. This is onto the reader's machine, is the run-starter's decision.
- A setup failure reports every test in the file as FAILED, not skipped. Teardown runs regardless, and its own failure fails the file.
- Every expanded instance always runs. There is no filtering, selection, or skip mechanism at any layer, by design.

## CI/CD

GitHub Actions workflow (`.github/workflows/ci.yml`) runs on push, with a job per thing it proves.
- `test` - installs bubblewrap, clears ubuntu-24.04's `kernel.apparmor_restrict_unprivileged_userns` (which otherwise denies bwrap the user namespace it needs, silently turning every bwrap test into a skip). Installs runs the bwrap probe as its own step so an unusable backend fails with its own error. Then builds the Go binary (multi-platform), runs tests via `wow-look-at-my/go-toolchain`, and creates releases on master pushes. The sandbox integration tests skip themselves when no backend is usable, so that probe step -- not any env knob -- is what stops a skip. This is from passing for isolation coverage in CI. The docker tests use the runner's own daemon and skip if the image cannot be fetched (a registry outage says nothing about the code). The publish step (job-level permissions REPLACE workflow-level ones) requires artifact-metadata: write`. The go-toolchain action carries the org guards this repo relies on, `no-tests-in-yaml` among them, so a test written into a `run:` script fails here. This is without a separate job
- `schema` - validates `testdata/schema/*.json` fixtures against `schema.json` using the `wow-look-at-my/json-validator` action, guarding against schema drift
- `every-host` - one matrix job over ubuntu/macos/windows, not a bespoke NT job: each leg runs THIS commit's APE (the `test` job's `go-build-test.broot` hand-off) over `examples/example.dats` with `--no-sandbox`. This is under the same script. The NT leg is what catches a missed call site in `runner/hostpath.go`'s backslash conversion, which unit tests can only reach by forcing `hostGOOS`. The posix legs are what make a red NT leg a DIFFERENCE between hosts rather than an unverified claim about one. `fail-fast: false`, because which hosts disagree is the finding
- `self-hosted-sandbox` - the CONSUMER path: this repo's own `action.yml` on the org's dind pool, running the PUBLISHED binary a caller will get. Privileged. As a result, it says nothing about the unprivileged case
- `action-every-host` - the action itself on ubuntu/macos/windows. The NT leg is the one that proves the WSL backend. Windows hosts no backend of its own, so `install-wsl-backend.sh` puts bubblewrap in a WSL distribution. The APE runs its Linux payload in there rather than anything relaxing -- docs/action.md. The published binary is a fat APE, so HOW it starts differs per host, and a raw spawn works only on Linux: Darwin answers `ENOEXEC`. NT finds an executable by extension. `every-host` cannot catch that -- it starts the binary from bash, which hides both. `fail-fast: false`, because which hosts disagree is the finding
- `native-backends` - `examples/sandbox.dats` under bwrap (ubuntu) and seatbelt (macos), on THIS commit's binary, with the backend installed by the same `.github/scripts/install-sandbox-backend.sh` a consumer gets. It exists because the backends can disagree about what a command may do -- scratch space was the first case. This is because no other job can catch that: `action-every-host` downloads the PUBLISHED binary. As a result, a runner-side sandbox fix is unprovable there until it has already merged, and `every-host` runs this commit's binary with `--no-sandbox`. NT is absent on purpose: its backend IS bwrap, inside WSL, so the ubuntu leg exercises the same one
- `sandbox-unprivileged-masked` - the sandbox itself, in a container this job BUILDS: unprivileged, `/proc` masked by docker, running the built of the binary THIS commit (the `test` job's `go-build-test.broot` hand-off). It deliberately does not borrow the org's slim fleet. That repo froze a merge gate that depends on another repo's deployed state can, and was. dats proves the mechanism. Webhooks' gha-runner smoke test proves the image

## Consuming dats from another repo's CI

`action.yml` at repo root makes this a composite GitHub Action: `uses: wow-look-at-my/dats@master` downloads the newest build from buildhost (never pinned). It runs it via a typed `tests:` input (files and directories, expanded to their top-level `*.dats` files) — see the README's "GitHub Actions" section. It wraps `wow-look-at-my/buildhost/.github/actions/buildhost-download` (`project: dats`), the same download every consumer used to hand-roll with curl/chmod. On Linux it also installs bubblewrap and, if it is blocked, clears Ubuntu 24.04's default `apparmor_restrict_unprivileged_userns` restriction the same way. This repo's own CI does (see "CI/CD" above). So a caller gets real sandboxing without needing `--no-sandbox` to work around the runner.

The action's surface is deliberately just `tests`/`working-directory`/`version`: there is no `args` passthrough and no way to disable the sandbox. The argv is built and sanitized in `.github/scripts/run-dats.ts` (a `wow-look-at-my/actions@typescript` script). That argv always carries `-v`, and there is no input to turn it off. A red leg has to name the test that failed and print its output, and a caller who must ask for that asks. This is after the run they needed it on.

## JSON Schema

`schema.json` provides IDE validation for `.dats` files. Can be used with YAML language servers.

> **Merge note (PR #73):** master moved the canonical agent guide into `AGENTS.md` (imported above). The guide below is this PR's updated copy of it, kept in `CLAUDE.md` because it carries the documentation of the in-progress XML migration (`schema/dats.xsd`, `internal/schema/`, `internal/runner/` — not yet wired into the CLI, so the active runtime still parses the YAML format). `AGENTS.md` is not part of this merge's conflict set, so the XML sections could not be folded into it here; when the migration lands, move them into `AGENTS.md` and shrink this file back to the import.

# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

DATS (Declarative Automated Testing System) is a Go CLI that runs tests defined in declarative YAML files (`.dats`). It natively executes commands, captures output, and verifies assertions without requiring external test frameworks.

## Build Commands

```bash
just generate       # Regenerate Go types from schema/dats.xsd (requires xgen)
just build          # Build the dats binary to build/dats (runs go fmt, go vet, go build)
just test           # Run Go tests with coverage + run example.dats
just install        # Symlink binary to ~/.local/bin/dats
```

## Running Specific Tests

```bash
# Run only Go unit tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run a .dats test file directly
./build/dats examples/example.dats

# Verbose mode (shows command details, full output on failure)
./build/dats -v examples/example.dats

# Parallel execution (4 workers; bare -j = one per CPU; -j 4 does NOT bind)
./build/dats -j4 examples/example.dats

# Sandboxing is ON by default (auto: bwrap, then seatbelt, then docker). Opt out per run:
./build/dats --no-sandbox examples/example.dats
./build/dats --sandbox=docker --sandbox-image=alpine:3.20 examples/example.dats

# Keep temp directory for debugging
./build/dats --keep-temp examples/example.dats

# Watch mode: run, then re-run the complete scope on file changes (Ctrl-C exits 0)
./build/dats watch examples/
```

## Architecture

[docs/architecture.md](docs/architecture.md) holds this section.

## DATS File Format

`docs/file-format.md` is the reference — every key, its accepted forms, its default, and the parse error it raises — ending in a complete field map under "Complete Field Reference". `schema.json` is the machine-readable copy, and `docs/examples.md` holds worked files. The properties an agent needs before opening any of them:

The in-progress XML migration (`internal/schema`/`internal/runner`, not yet wired into the CLI) targets this format:

- Indentation is TABS (`docs/file-format.md#yaml-dialect`). A sequence item's sibling keys align with multiple spaces AFTER the tab.
- A file may only NARROW its sandbox. `sandbox: false` and `ssh: false` are parse errors naming `--no-sandbox`. Turning isolation off, or pulling a remote command back onto the reader's machine, is the run-starter's decision.
- A setup failure reports every test in the file as FAILED, and never skipped. Teardown runs regardless, and its own failure fails the file.
- Every expanded instance always runs. There is no filtering, selection, or skip mechanism at any layer, by design.

### File-Level Properties

| Property | Required | Description |
|----------|----------|-------------|
| `shared.files` | No | Map of filename → content, written once per file into `shared/` before setup; contents expand `{shared.X}` only; names must be local relative paths |
| `setup` | No | Hook command or list (bare string, or a mapping of `cmd`/`env`/`stdin_file`/`timeout`), run once in order before the file's tests; `cmd`/`env` expand `{shared.X}` only, bounded by `timeout` (default 30s, must be > 0). A failure skips remaining setup commands and reports EVERY test as failed (reason `file setup failed`, never "skipped"); teardown still runs |
| `sandbox` | No | A mapping (`network`, `image`) that narrows the sandbox for this file's commands (tests AND setup/teardown). File-level only -- one file's commands share one temp dir and one hook lifecycle. The CLI is the outer bound: under `--no-sandbox` the block is inert, and nothing in a file can turn its own sandbox off (`sandbox: false` is a parse error pointing at `--no-sandbox`) |
| `ssh` | No | `[user@]host` this file's commands run on (tests AND setup/teardown). A REQUEST, not a decision: the (file, host) pair is approved once and stored in `~/.config/dats/ssh-trust.json` (`dats trust`), and a typed `--ssh` outranks it (announced, never swapped silently). `ssh: false` is a parse error -- local is the MORE privileged side, so a file pulling a command here is the same harm `sandbox: false` prevents. A remote run has NO sandbox and the working directory does not travel |
| `teardown` | No | Same hook command or list form as `setup`, always run once in order after the file's tests (after failures, even after setup failure; one failure does not stop the rest). Any failure marks the file failed (exit 1) even when all tests passed |

```xml
<dats>
  <test desc="optional description" cmd="command to run" exit="0">
    <!-- Input: stdin content piped to cmd -->
    <stdin>input text</stdin>

    <!-- Input: fixture files created before running cmd -->
    <input name="file.txt">content</input>

    <!-- Output: stdout assertions -->
    <stdout>
      <match>pattern</match>           <!-- Substring match -->
      <not-match>error</not-match>     <!-- Must NOT appear -->
      <line n="0">^first line$</line>  <!-- Line-specific regex (0-indexed) -->
    </stdout>

    <!-- Output: stderr assertions -->
    <stderr>
      <match>warning</match>
    </stderr>

    <!-- Output: file assertions -->
    <output name="result.txt" exists="true">
      <match>expected content</match>
      <not-match>error</not-match>
    </output>
  </test>
</dats>
```

### Test Attributes

| Attribute | Required | Description |
|-----------|----------|-------------|
| `cmd` | Yes | Command to run. Use `{inputs.X}` and `{outputs.X}` for file paths |
| `desc` | No | Description for the test (used in output) |
| `exit` | No | Expected exit code (default: 0). Int 0-255 (bare or quoted, e.g. `"3"`) or `EXIT_SUCCESS`/`EXIT_FAILURE`; floats rejected at parse time |
| `timeout` | No | Per-test timeout: int seconds (bare or quoted, e.g. `"5"`) or Go duration string (e.g. `500ms`, `2s`). 0/omitted = no timeout; floats rejected (write `1.5s`, not `1.5`) |
| `matrix` | No | Map of variable name → list of scalar values; expands the test into one instance per combination (cartesian product, declaration order, last variable varies fastest). `{matrix.X}` substitutes in desc, cmd, stdin, file contents, env values, and output patterns; every instance always runs, reported as `desc [k=v, ...]` |
| `ssh` | No | `[user@]host` this ONE test runs on instead of the file's target. Legal only in a file that declares its own `ssh:` -- a per-test target may only move a command between remote hosts, never back onto the reader's machine. Approved separately (a file naming two hosts needs two approvals), gets its own temp dir plus a push-only mirror of `shared/`, and takes `{matrix.X}` so one test fans across a fleet. `setup`/`teardown`/`shared/` still only run on the FILE's target, so a setup that prepares a service prepares only that host |
| `inputs.stdin` | No | Content piped to command's stdin |
| `inputs.files` | No | Map of filename → content (creates fixture files) |
| `inputs.env` | No | Map of env var name → value, added to the inherited environment (values go through placeholder expansion) |
| `outputs.stdout` | No | Patterns to match in stdout |
| `outputs.stderr` | No | Patterns to match in stderr |
| `outputs.!stdout` | No | Patterns that must NOT appear in stdout |
| `outputs.!stderr` | No | Patterns that must NOT appear in stderr |
| `outputs.files` | No | Map of filename → FileCheck for output file validation; empty check (`{}`/null) = must exist |
| `outputs.!files` | No | Map of filename → FileCheck with each check inverted (e.g. `exists: true` = must NOT exist; empty check = must NOT exist) |
| `outputs.snapshot` | No | Golden-file assertion: `true` (snapshot stdout) or map of stream booleans (`stdout`/`stderr`, at least one true). Captured output must byte-match `<file>.snapshots/NNN-<slug>.<stream>.golden` after temp-path normalization; `--update` rewrites goldens (skipping instances with other failures) and prunes stale ones |
| `outputs.json_output` | No | Expected JSON value of the whole stdout (deep equality; object keys order-insensitive, arrays order-sensitive, numbers by value) |

### Test Children (XML migration — not yet wired into the CLI)

| Element | Description |
|---------|-------------|
| `<stdin>` | Content piped to command's stdin |
| `<input name="X">` | Fixture file created before running cmd |
| `<stdout>` | Stdout assertions (`<match>`, `<not-match>`, `<line>`) |
| `<stderr>` | Stderr assertions (`<match>`, `<not-match>`, `<line>`) |
| `<output name="X">` | Output file validation with optional `exists` attr |

## CI/CD

GitHub Actions workflow (`.github/workflows/ci.yml`) runs on push, with a job per thing it proves.
- `test` - installs bubblewrap, clears ubuntu-24.04's `kernel.apparmor_restrict_unprivileged_userns` (which otherwise denies bwrap the user namespace it needs, silently turning every bwrap test into a skip). It runs the bwrap probe as its own step so an unusable backend fails with its own error. Then builds the Go binary (multi-platform), runs tests via `wow-look-at-my/go-toolchain`, and creates releases on master pushes. The sandbox integration tests skip themselves when no backend is usable. That probe step -- not any env knob -- is what stops a skip from passing for isolation coverage in CI. The docker tests use the runner's own daemon and skip if the image cannot be fetched (a registry outage says nothing about the code). The publish step requires `artifact-metadata: write` (job-level permissions REPLACE workflow-level ones). The go-toolchain action carries the org guards this repo relies on, `no-tests-in-yaml` among them. A test written into a `run:` script fails here without a separate job
- `schema` - validates `testdata/schema/*.json` fixtures against `schema.json` using the `wow-look-at-my/json-validator` action, guarding against schema drift
- `every-host` - one matrix job over ubuntu/macos/windows, not a bespoke NT job. Each leg runs THIS commit's APE (the `test` job's `go-build-test.broot` hand-off) over `examples/example.dats` with `--no-sandbox`, under the same script. The NT leg is what catches a missed call site in `runner/hostpath.go`'s backslash conversion, which unit tests can only reach by forcing `hostGOOS`. The posix legs are what make a red NT leg a DIFFERENCE between hosts rather than an unverified claim about one. `fail-fast: false`, because which hosts disagree is the finding
- `self-hosted-sandbox` - the CONSUMER path: this repo's own `action.yml` on the org's dind pool, running the PUBLISHED binary a caller will get. Privileged, so it says nothing about the unprivileged case
- `action-every-host` - the action itself on ubuntu/macos/windows. The NT leg is the one that proves the WSL backend. Windows hosts no backend of its own, so `install-wsl-backend.sh` puts bubblewrap in a WSL distribution. The APE runs its Linux payload in there rather than anything relaxing -- docs/action.md. The published binary is a fat APE, so HOW it starts differs per host, and a raw spawn works only on Linux. Darwin answers `ENOEXEC` and NT finds an executable by extension. `every-host` cannot catch that -- it starts the binary from bash, which hides both. `fail-fast: false`, because which hosts disagree is the finding
- `native-backends` - `examples/sandbox.dats` under bwrap (ubuntu) and seatbelt (macos), on THIS commit's binary, with the backend installed by the same `.github/scripts/install-sandbox-backend.sh` a consumer gets. It exists because the backends can disagree about what a command may do -- scratch space was the first case -- and. This is because no other job can catch that: `action-every-host` downloads the PUBLISHED binary. A runner-side sandbox fix is unprovable there until it has already merged, and `every-host` runs this commit's binary with `--no-sandbox`. NT is absent on purpose: its backend IS bwrap, inside WSL, so the ubuntu leg exercises the same one
- `sandbox-unprivileged-masked` - the sandbox itself, in a container this job BUILDS: unprivileged, `/proc` masked by docker, running the built of the binary THIS commit (the `test` job's `go-build-test.broot` hand-off). It deliberately does not borrow the org's slim fleet. That repo can freeze a merge gate that depends on another repo's deployed state, and was. dats proves the mechanism. Webhooks' gha-runner smoke test proves the image

## Consuming dats from another repo's CI

`action.yml` at repo root makes this a composite GitHub Action: `uses: wow-look-at-my/dats@master` downloads the newest build from buildhost (never pinned). It runs it via a typed `tests:` input (files and directories, expanded to their top-level `*.dats` files) — see the README's "GitHub Actions" section. It wraps `wow-look-at-my/buildhost/.github/actions/buildhost-download` (`project: dats`), the same download every consumer used to hand-roll with curl/chmod. On Linux it also installs bubblewrap. Suppose it is blocked. Then it clears Ubuntu 24.04's default `apparmor_restrict_unprivileged_userns` restriction the same way.

This repo's own CI does (see "CI/CD" above) — so a caller gets real sandboxing without needing `--no-sandbox` to work around the runner. The action's surface is deliberately just `tests`/`working-directory`/`version`: there is no `args` passthrough and no way to disable the sandbox. The argv is built and sanitized in `.github/scripts/run-dats.ts` (a `wow-look-at-my/actions@typescript` script). That argv always carries `-v`, and there is no input to turn it off. A red leg has to name the test that failed and print its output. A caller who must ask for that asks after the run they needed it on.

## JSON Schema

`schema.json` provides IDE validation for `.dats` files. Can be used with YAML language servers.
