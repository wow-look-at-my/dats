# CLI Usage

## Synopsis

```
dats [test] [files-or-dirs...] [flags]
dats watch [files-or-dirs...] [flags]
dats syntax [files-or-dirs...] [flags]
dats version
```

`dats` runs tests directly; there is no separate build/generate step. Arguments may be `.dats`
files or directories in any mix. If no arguments are given, `.dats` files are discovered from
the current directory tree.

## Commands

| Command | Description |
|---------|-------------|
| `test` | Run tests from the given `.dats` files or directories. This is the default action, so `dats file.dats` and `dats test file.dats` are equivalent. |
| `watch` | Run tests like `test`, then keep watching the resolved files and re-run the complete argument scope on every change (see [Watch Mode](#watch-mode-dats-watch)). |
| `syntax` | Parse and validate `.dats` files without executing any tests. Accepts the same file/directory arguments. |
| `version` | Print a one-line `dats <version>` (also available as the `--version` flag). |

## File Arguments and Discovery

- A **file** argument must have the `.dats` extension. Explicitly named files are always
  accepted, even hidden ones (leading `.`).
- A **directory** argument (symlinks to directories included) is searched recursively with the
  same rules as no-arg discovery; a directory that yields nothing is an error:
  `no .dats files found in <dir>`.
- **Discovery** (no-arg and directory args) skips hidden directories and hidden `.dats` files
  — except the walk root itself, so running inside a dotted directory still works. Paths that
  cannot be walked (e.g. unreadable directories) print `warning: skipping <path>: <err>` on
  stderr and discovery continues.
- Arguments are **deduplicated** by absolute path (first-seen order), so a file named twice —
  or both named and covered by a directory argument — runs exactly once.
- A nonexistent or inaccessible argument is an error: `cannot access <arg>: <err>`.

## Flags

| Flag | Description |
|------|-------------|
| `-v, --verbose` | Show command details, durations, and full output on failure |
| `-j, --jobs[=N]` | Run test commands in parallel with N workers (bare `-j` = one per CPU). Attach the value — `-jN`, `-j=N`, or `--jobs=N`; a space-separated `-j N` does not bind (see [Parallel Execution](#parallel-execution--j)). Without the flag, execution is fully serial |
| `--report-junit <path>` | Write a JUnit XML report of the run to `<path>` (see [Report Files](#report-files)) |
| `--report-json <path>` | Write a JSON report of the run to `<path>` (see [Report Files](#report-files)) |
| `--update` | Rewrite snapshot golden files from actual output instead of failing, and prune stale ones (see [Updating Snapshots](#updating-snapshots---update)) |
| `--sandbox <mode>` | Sandbox backend for test commands: `auto` (default — bwrap, then seatbelt, then docker), `bwrap`, `seatbelt`, `docker`, or `none` (see [Sandboxing](#sandboxing---sandbox)) |
| `--no-sandbox` | Run test commands directly on the host; same as `--sandbox=none`. Combining it with a different `--sandbox` value is an error |
| `--sandbox-image <ref>` | Container image the docker backend runs commands in (default `debian:stable-slim`) |
| `--keep-temp` | Keep the per-run temp directory (prints its path) for debugging |
| `--coverdir <dir>` | Set `GOCOVERDIR` on executed commands — tests and file-level setup/teardown alike — to collect coverage data |
| `--version` | Print `dats <version>` and exit |

## Examples

```bash
# Run a specific file (positional, or via the test subcommand)
dats test.dats
dats test test.dats

# Run every .dats file under a directory
dats test tests/

# Run every .dats file under the current directory
dats test

# Verbose output
dats -v test.dats

# Parallel execution: 4 workers / one worker per CPU
dats -j4 tests/
dats -j tests/

# Machine-readable reports (either or both; parent dirs are created)
dats --report-junit report.xml tests/
dats --report-json report.json tests/

# Rewrite snapshot golden files from actual output
dats --update tests/

# Watch mode: run everything, then re-run on file changes (Ctrl-C to exit)
dats watch tests/

# Keep the temp directory to inspect fixtures/outputs
dats --keep-temp test.dats

# Validate syntax without running
dats syntax test.dats
dats syntax tests/     # validate all .dats files under a directory
dats syntax            # validate all .dats files in the tree

# Version
dats version
dats --version

# Help
dats -h
dats <command> -h
```

## Sandboxing (--sandbox)

**Test commands are sandboxed by default.** A `.dats` file is a list of shell commands from
whoever wrote the file; running them straight on your machine is a choice, so dats makes you
make it. `--sandbox=auto` (the default) uses the platform's native sandbox — bubblewrap on
Linux, `sandbox-exec` (seatbelt) on macOS — and falls back to docker where neither works.

Every command a file runs is sandboxed — test instances **and** the file-level
`setup`/`teardown` hooks. A file cannot use its hooks as an unsandboxed side door.

### Backends

| | `bwrap` (Linux) | `seatbelt` (macOS) | `docker` (fallback) |
|---|---|---|---|
| Enforced by | user namespaces + mounts | an SBPL profile via `sandbox-exec` | container isolation |
| Filesystem | the host's, bound **read-only** | the host's, **writes denied** | the **image's**, writable but discarded on exit |
| Writable | the file's temp dir (fixtures, `{outputs.X}`, `{shared.X}`) + declared extras | same | same, bind-mounted from the host |
| Working directory | the host's, read-only | the host's, read-only | the host's, bind-mounted read-only, and `-w` into it |
| Available tools | the host's — a sandboxed run behaves like an unsandboxed one minus the writes | the host's, same as bwrap | the image's only; host binaries and libraries are **not** there |
| Environment | inherited as usual | inherited as usual | the image's, plus this run's `inputs.env` values and `GOCOVERDIR` |
| Processes | own PID namespace, dies with dats | not isolated — the profile governs files and network, not the process table | own container, killed when the command is |
| `sandbox.image` | ignored | ignored | the image commands run in |
| Overhead | ~5 ms per command | ~5 ms per command | ~350 ms per command |

All three make the file's temp directory writable, so fixtures, `{outputs.X}` assertions and
snapshots work unchanged. The differences that bite: under bwrap a write to any host path
outside the temp directory fails with `Read-only file system` and under seatbelt with
`Operation not permitted`; under docker it "succeeds" into the container's own throwaway
filesystem and never reaches the host.

The two native backends are platform-exclusive — `bwrap` does not exist on macOS and
`sandbox-exec` does not exist on Linux — so `auto` really resolves to "this platform's native
sandbox, else docker". Asking for the wrong one by name is an error, never a silent
substitution.

### Selection and failure

Detection is lazy and cached — probed at most once per run, and only when a file actually
needs a sandbox, so a corpus whose files all opt out runs on a machine with no backend at
all. Every probe exercises what it will use: bubblewrap is routinely installed on kernels
that deny it the user namespace it needs, `sandbox-exec` ships on every mac but is refused
in some hardened contexts, and the docker CLI is routinely installed with no daemon behind
it.

When no backend can be provided, the run **fails** — it never quietly falls back to the host:

```
Error: running tests.dats: no usable sandbox backend: bwrap: not found in $PATH; sandbox-exec: not found in $PATH; docker: not found in $PATH
install bubblewrap (Linux), or start docker, or opt out with --no-sandbox (or `sandbox: false` in the file)
```

An explicitly requested backend never falls back either: `--sandbox=bwrap` gets bubblewrap or
an error — including on macOS, where it can only ever be an error.

### Opting out

- `--no-sandbox` (or `--sandbox=none`) for a whole run.
- `sandbox: false` in a file whose commands genuinely need the host — see
  [file-format.md](file-format.md#sandbox). The same block can also cut the network, pick the
  docker image, or declare extra writable host paths.

The flag is the outer bound: a file can narrow what the CLI selected, never widen it. Under
`--no-sandbox`, a file's `sandbox:` block is inert.

### What it does and does not isolate

- **Writes** are confined to the file's temp directory (plus anything declared writable).
- **Reads are not restricted**: under bwrap and seatbelt the whole host filesystem stays
  readable, including your home directory and its secrets. This protects your machine from a
  test, not your secrets from one.
- **The network is shared** unless a file sets `network: false`.
- A command killed by a signal inside bwrap is reported as exit `128+N` rather than as a
  signal death, because bwrap exits that way on its child's behalf. Timeouts are unaffected —
  they kill through the sandbox and still report as timeouts.
- **seatbelt confines files and network, not processes.** There is no PID namespace: a
  sandboxed command can still see the host's process table. It is the file-write and network
  boundary that is enforced.

## Parallel Execution (-j)

`-j`/`--jobs` runs test commands in parallel. Without the flag, execution is fully serial —
the historical behavior, unchanged.

### Flag forms

- Bare `-j` or `--jobs` — one worker per CPU.
- `-jN`, `-j=N`, `--jobs=N` — exactly N workers. An explicit N below 1 is an error.
- **The space-separated forms do not work**: with an optional flag value, `-j 4` and
  `--jobs 4` parse as bare `-j` (one worker per CPU) plus a positional argument `4` — the
  same trap as GNU make. Since `4` is not a `.dats` file, the run fails with
  `cannot access 4`; attach the value instead.

### Scheduling

- **One global pool.** At most N workload commands run at once across ALL files — and
  file-level setup/teardown commands occupy pool slots exactly like test commands, so no
  more than N spawned processes ever exist at once.
- **Per-file barriers are preserved.** Within each file, shared files are written and setup
  commands run (sequentially, in declared order) before any of that file's test instances
  start; a setup failure reports every instance as failed (`file setup failed`, never
  "skipped") without running them; teardown commands run (sequentially, in declared order)
  only after the file's last instance finishes, and always run. Test instances of one file
  may run concurrently with each other and with other files' instances and hooks.
- **Everything still runs.** `-j` changes scheduling only — there is no test filtering or
  selection, and per-test timeouts, exit-code semantics, fixture isolation, and
  `--coverdir` behave identically to a serial run.

### Output and determinism

Output is buffered and printed in canonical order — files in the order given on the command
line (or discovered), instances in expansion order within each file — regardless of
completion order. A `-j` run's output is byte-identical to a serial run of the same corpus
whenever the outcomes are equal; summary counts and the process exit code are computed
identically. (Under `-v`, reported durations naturally vary between runs.)

### Priority

Every spawned workload command — test instances and setup/teardown hooks — runs at low OS
priority (nice 19 applied to the command's process group) so a heavily parallel run does
not starve the machine; `dats` itself stays at normal priority. This is best-effort and
unix-only (a no-op on Windows); renice failures are ignored. Serial runs never touch
process priority.

### Error handling

In jobs mode every file is parsed up front, so a parse error in any file aborts the run
before a single command executes. (Serial mode parses each file as it reaches it: a bad
later file aborts the run after earlier files already ran. This error-path difference is
the only behavioral divergence.)

## Report Files

`--report-junit <path>` and `--report-json <path>` write machine-readable reports of the
run — for CI systems and editor integrations — at the end of the run. Either or both may
be given; parent directories are created automatically.

- Reports are written whenever the run executed, **including failing runs** (the process
  still exits 1); they are built from the same data in serial and `-j` runs, in canonical
  order. Stdout output and exit codes are unchanged.
- Hard errors that abort the run (unreadable file, parse error) write no report; a report
  file that cannot be written is itself an error (stderr message, exit 1) even when every
  test passed. `dats syntax` never writes reports.
- JSON summary counts equal the CLI summary numbers (test instances only); JUnit totals
  additionally include synthetic `[setup]`/`[teardown]` cases for failed file-level hooks.

See [Machine-Readable Reports](reports.md) for the full format specification and its
stability guarantees.

## Updating Snapshots (--update)

Tests with an `outputs.snapshot` assertion compare captured output against golden files
stored next to the `.dats` file (see
[Snapshot Assertions](file-format.md#snapshot-assertions-outputssnapshot)). `--update`
rewrites those goldens from actual output instead of failing:

```
$ dats --update demo.dats
Running demo.dats (2 tests)

ok 1 - renders the report
  # updated golden: demo.snapshots/001-renders-the-report.stdout.golden
ok 2 - split streams
  # updated golden: demo.snapshots/002-split-streams.stdout.golden
  # updated golden: demo.snapshots/002-split-streams.stderr.golden
# pruned stale golden: demo.snapshots/003-renamed-away.stdout.golden

2/2 passed

Updated 3 golden file(s), pruned 1 stale
```

- Only missing or differing goldens are written; up-to-date goldens are untouched and
  unlisted (a fully clean run prints no goldens summary at all).
- An instance with any **other** failure neither writes nor compares its goldens — goldens
  never update from a failing run.
- Stale `*.golden` files (tests renamed, reordered, or removed; streams disabled) are
  pruned, and an emptied snapshot directory is removed. Non-`.golden` files are never
  touched.
- `dats syntax` accepts `--update` but ignores it — nothing runs, so nothing is updated.

## Watch Mode (dats watch)

`dats watch [files-or-dirs...]` resolves the same targets as `dats test`, runs them, then
keeps watching and re-runs on every relevant change:

```bash
dats watch demo.dats     # one file
dats watch tests/        # a directory tree
dats watch               # everything under the current directory
```

### What is watched

- The **resolved `.dats` files** (via their parent directories, so editors that save by
  rename-replace still trigger).
- Each resolved file's **`.snapshots/` golden directory** (when it exists) — goldens are
  test inputs, so editing one re-runs.
- Every **directory argument** (and the current directory in no-arg mode), recursively —
  with the same hidden-directory skip rules as discovery — so newly created `.dats` files
  and new subdirectories are picked up and join the scope.

Changes are **debounced** (250ms after the last event), so an editor's save burst causes
one re-run; changes landing while a run is in flight coalesce into exactly one follow-up
run. Chmod-only events are ignored.

### The complete scope re-runs, every time

Each re-run executes the **complete original argument scope**, never a subset. dats has no
test filtering or selection by design — every instance always runs — and `watch` adds no
narrowing flags. The obvious alternative (re-running only the changed file) was considered
and rejected: it is test selection through the back door, and cross-file signals like the
combined total would silently stop covering the scope you asked for. Runs are cheap and
complete beats clever.

A run that fails — including a `.dats` file made temporarily unparseable mid-edit — never
kills the watch: the error is reported and watching continues; fixing the file triggers
the next run.

### Terminal UX

- On a TTY, the screen is cleared before every run; otherwise runs are separated by a
  `----------------------------------------` line (except before the first).
- Every run starts with `# watch: run N at HH:MM:SS`, suffixed ` (initial)` on the first
  run or ` (changed: <up to 3 paths>, +K more)` afterwards, and ends with
  `# watch: waiting for changes (Ctrl-C to exit)`.

### Exiting

Ctrl-C (or SIGTERM) prints `# watch: interrupted, exiting` and **exits 0** — watch's exit
code never reflects test outcomes. If a run is in flight, the in-flight commands' whole
process groups are killed promptly, the interrupted file's `teardown` still runs (the
always-runs contract holds even here), and the aborted run's outcome is discarded.

### Composing with other flags

`watch` inherits every flag and adds none:

- `-v`, `-j` — verbose and parallel runs behave exactly as under `dats test`.
- `--report-junit`/`--report-json` — the report files are rewritten after every run; report
  writes never retrigger the watcher, even when the report lives inside a watched
  directory.
- `--update` — every run rewrites stale goldens. While `--update` is set, golden-file
  changes do not trigger runs (the run itself writes goldens, so reacting would
  self-retrigger — and a re-run would be a no-op anyway, since goldens are rewritten from
  actual output).

### Caveat

Watching relies on OS filesystem notifications (fsnotify). Network and remote filesystems
(NFS, SMB, some container mounts) may not deliver change events — on such mounts, edits
may not trigger re-runs.

## Output

Results are printed in a TAP-like format:

```
# sandbox: bwrap
Running test.dats (3 tests)

ok 1 - echo test
not ok 2 - line matching
  # stdout: line 0: expected to match "^line0$", got "wrong"
ok 3 - exit code test

2/3 passed, 1 failed
```

The `# sandbox:` line precedes each file's header and names the backend that file's commands
ran under (`# sandbox: docker debian:stable-slim`, plus ` (no network)` when the file cut the
network). A file whose commands ran on the host prints no such line, so an unsandboxed run's
output is byte-for-byte what it has always been.

The process exits `0` when all tests pass and `1` when any test fails. When multiple files are
run, a combined total is printed at the end.

Failing runs never dump the usage text, and CLI errors are printed exactly once to stderr
(`Error: <message>`) — test and syntax failures exit `1` silently, since the runner output
above already reported them.

## Build System Integration

### Just

```just
test:
    dats test tests/
```

### Make

```makefile
test:
	dats test tests/
```
