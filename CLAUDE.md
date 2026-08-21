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

### Core Flow
1. `.dats` YAML file is parsed using [yaml-fixed](https://github.com/wow-look-at-my/yaml-fixed) — tabs-only indentation, no
   anchors/aliases, no tags (so the negated keys are written bare: `!stdout:`, quoted still accepted), canonical scalar
   reformatting; see `docs/file-format.md#yaml-dialect`
2. Every test is expanded up front into its matrix instances (`schema.ExpandMatrix`; non-matrix tests = one instance) — the header count, instance numbering, temp dirs, summary counts, and setup-failure reporting all operate on the expanded list; every instance always runs (no test filtering/selection by design)
3. Per file: the file's sandbox is resolved (`Runner.newSandboxPlan`) BEFORE anything runs — a file that must be sandboxed and cannot be fails outright; then a `shared/` dir is created, `shared.files` are written into it, and `setup` commands run in order (a failure fails EVERY test instance in the file — reported as failures, never "skipped" — but teardown still runs)
4. For each test instance, fixtures are set up in a temp directory
5. Command is executed via `bash -c` — inside the file's sandbox unless the RUN opted out (`runner/sandbox.go`; the wrapper ends in the same `bash -c`) — with placeholder expansion
6. Exit code, stdout, stderr, and output files are validated against assertions; `outputs.snapshot` additionally byte-compares captured streams against golden files in `<file>.snapshots/` next to the .dats file (temp paths normalized to `{testdir}`/`{shareddir}`/`{tmproot}` tokens), and `--update` rewrites those goldens from actual output (never from an instance with other failures) and prunes stale ones
7. `teardown` commands always run in order (after test failures and even when setup failed); any teardown failure marks the file failed (exit 1) even when all tests passed
8. Results are printed in TAP-like format
9. Execution is ALWAYS concurrent — there is one execution path, not a serial one and a parallel one. `-j`/`--jobs` only sizes the pool: absent = one per logical CPU, `-j1` = one command at a time. One global N-slot pool bounds every spawned command (instances and hooks) across all files, per-file barriers are preserved, spawned commands are reniced to 19 (unix, best-effort), and output is buffered and printed in canonical order — the bytes depend only on outcomes, never on `-j` or on scheduling
10. With `--report-junit`/`--report-json`, runTests writes report files from the finished results at end of run — always when the run executed (especially failing runs; identical data serial and `-j`), never on hard errors that abort the run; a report write failure is itself an error (stderr, exit 1). Formats and stability contract: `docs/reports.md`
11. `dats watch` wraps the same pipeline in an fsnotify loop: after each run it waits for relevant changes (resolved `.dats` files, their `.snapshots/` golden dirs, directory args recursively; 250ms debounce) and re-runs the COMPLETE original argument scope — never a subset (no test filtering by design, no narrowing flags). Ctrl-C/SIGTERM exits 0: the context plumbed through the runner kills in-flight process groups, teardown still runs, the aborted outcome is discarded

### Go Package Structure
- `dats.go` (module root, `package dats`) - **The library API, and the product's front door**: `Options`/`Sandbox`/`Result` + `Run`, plus `FindFiles` and `Validate`. Everything a run does lives here or below; the binary is a thin flag-parsing wrapper, so a library caller gets byte-identical behavior instead of a reimplementation. Contract: `Run` errors only when the RUN could not be carried out (bad path, parse failure, unusable sandbox, unknown mode) -- failing TESTS are a `Result`, never an error, and `Result.Ok()` is the verdict (false on a teardown failure even with zero failed tests). The zero `Sandbox` is AUTO, not none: a caller that says nothing gets isolation, and opting out is spelled `Sandbox{Mode: runner.SandboxNone}`. `Options.Env` entries reach every command (hooks included), an empty value clearing an inherited variable. See `docs/library.md`
- `find.go` (root) - Resolves file/directory args (dirs recurse; symlinked dir roots followed) or discovers `.dats` files in the tree; skips hidden dirs/files, dedupes by absolute path. Exported as `FindFiles` -- one implementation for the library, `test`, `syntax`, and `watch`
- `internal/paths/` - `Dedupe` (by absolute path, first spelling wins), shared by discovery and the watch scope
- `cmd/dats/main.go` - Minimal entry point; calls `cmd.Execute()`
- `cmd/` - Cobra CLI commands (each command self-registers in its own file)
  - `root.go` - Root command and persistent flags (`--verbose`, `--keep-temp`, `--coverdir`, `-j`/`--jobs`, `--report-junit`/`--report-json`, `--update`, `--sandbox`/`--no-sandbox`/`--sandbox-image`); failing runs exit 1 without usage dumps, errors print exactly once
  - `jobs.go` - The `-j`/`--jobs` flag: registration (int flag DEFAULTING to NumCPU, with the same NoOptDefVal so bare `-j` works), make-style `-jN` argv normalization to `--jobs=N` (pflag resolves NoOptDefVal before the attached `-farg` form, so a raw `-j4` would fail; space-separated `-j 4` intentionally leaves `4` positional, as in GNU make), and resolution (absent → NumCPU; any N < 1, including an explicit `-j0`, → error)
  - `report.go` - The `--report-junit`/`--report-json` flags (long-only, value required) and the write-to-disk plumbing (MkdirAll parent dirs, attempt both files, errors.Join); rendering lives in the `report` package. runTests measures the execution wall time and calls writeReports after totals — always when the run executed, never on hard errors that abort it
  - `update.go` - The `--update` flag (long-only bool): rewrite snapshot golden files instead of failing. Plumbed into `Runner.Update` by runTests, which also prints the end-of-run goldens summary (`Updated N golden file(s)[, pruned M stale]`, silent when nothing changed); `dats syntax` accepts the flag but ignores it
  - `sandbox.go` - The `--sandbox` (auto|bwrap|seatbelt|docker|none, default auto), `--no-sandbox` (alias for none; contradicting `--sandbox` is an error), and `--sandbox-image` flags, plus `resolveSandbox` → `*runner.SandboxConfig` (nil = opted out; `Image` carries only a TYPED `--sandbox-image`, via `flags.Changed`, so an untouched flag stays "" = nobody chose). Nothing is probed here — resolution is lazy, so `--no-sandbox` and `dats syntax` run with no backend installed. This is the ONLY opt-out; a file cannot disable its own sandbox
  - `test.go` - `test` subcommand (also the default action): maps the parsed flags onto `dats.Options`, calls `dats.Run`, writes any requested report files (a write failure is a real error even when tests passed), and turns `!Ok()` into the silent `errTestsFailed` sentinel Execute exits 1 on. Execution, discovery, totals and the goldens summary all belong to the library -- do NOT reimplement them here. A nil `*runner.SandboxConfig` from `resolveSandbox` means opted out, so it maps to the EXPLICIT `SandboxNone` (the library's zero value is auto). runTests takes a context (plain `dats test` passes context.Background() — no signal handling; `dats watch` passes its interrupt context)
  - `watch.go` - `watch` subcommand: initial run, then fsnotify-driven re-runs of the complete original argument scope (the repo's only filesystem-watch dependency, `github.com/fsnotify/fsnotify`). Watches directories, not files (editor rename-replace safe): resolved files' parent dirs, their existing snapshot golden dirs, and directory args recursively (non-hidden, like discovery). Each cycle re-resolves the scope (keeping the previous list + watch set on resolution errors) and rebuilds the watcher before running, so mid-run changes coalesce into one pending re-run (250ms debounce via `watchLoop`, a pure loop core driven by an event channel). Relevance is a pure function (`watchScope.relevantChange`): chmod-only events, report-file paths, and goldens under `--update` never retrigger (self-retrigger prevention); new directories under a tree force a re-cycle so they get watched. TTYs get a screen clear + `# watch: run N at HH:MM:SS` header, non-TTYs a dashed separator; Ctrl-C/SIGTERM (signal.NotifyContext) always exits 0, discarding an aborted run's outcome; parse/run errors never kill the watch
  - `syntax.go` - `syntax` subcommand: validates `.dats` files without running them (the sandbox flags are inherited but inert here, like `--update`: nothing runs, so there is nothing to sandbox)
  - `version.go` - `version` subcommand and `--version` flag: one-line `dats <version>` from build info
- `schema/` - YAML schema types + parser (public, importable by external modules)
  - `types.go` - Schema types with custom unmarshalers
  - `parse.go` - `ParseFile`: reads and validates a `.dats` file (rejects unknown keys, multi-document YAML, non-local
    fixture names, undeclared `{matrix.X}` references, matrix placeholders in setup/teardown/shared, a banned redirect in
    cmd/setup/teardown (`bannedRedirect`, types.go -- a heredoc `<<WORD` or a herestring `<<<`, each with its own message),
    and a `copy` destination that is non-local, empty-sourced, or collides with a `files` entry of the same name
    (`validateCopyBlock`, shared across `shared.copy` and `inputs.copy`)
  - `sandbox.go` - `SandboxSpec`: the file-level `sandbox` key (mapping of `network`/`image`, strictly validated — unknown/duplicate keys, wrong types, non-mappings, and an empty mapping are parse errors) plus the nil-safe `NetworkEnabled` accessor (unstated = network on). A file can only NARROW its sandbox: `sandbox: false` and `enabled:` are parse errors naming `--no-sandbox`, so a file cannot take isolation away from whoever runs it
  - `matrix.go` - `Matrix` (declaration-ordered variables, strict value validation), `ExpandMatrix` (cartesian instance expansion, deep copies, single-pass `{matrix.X}` substitution), and the single definition of the matrix substitution scope shared by validation and expansion
- `runner/` - Native test runner (public, importable by external modules). RunFile/RunFiles/RunTest/Execute take a context: cancellation kills in-flight process groups (surfacing as signal deaths, never as timeouts); teardown runs under context.WithoutCancel so it always executes
  - `runner.go` - Orchestrates test execution; `Runner.Env` holds run-wide extra `KEY=VALUE` entries applied to every command (a test's own `inputs.env` is applied after, so a file wins; an empty value clears an inherited variable) and `RunFiles` copies it into each per-file Runner (RunFile, RunTest — both context-first; setup and instances use the live ctx, teardown a context.WithoutCancel derivative); `RunFile` parses and hands `runFile` its own NumCPU-sized pool, while `RunFiles` hands every file ONE shared pool — that is the only difference between a single-file and a multi-file run. runFile also writes shared fixtures, runs setup (stops at first failure; then every test is reported failed with reason "file setup failed"), and always runs all teardown commands (`runHookCommand` executes one `schema.HookCommand` via the same bash path and env construction as test commands — including `GOCOVERDIR` under `--coverdir`, plus the entry's own `env` (sorted, `{shared.X}`-expanded) and `stdin_file` content, bounded by `HookCommand.EffectiveTimeout()` — with `{shared.X}`-only expansion of `cmd`/`env`)
  - `files.go` - Multi-file orchestration (`RunFiles`; each per-file Runner inherits the shared `Sandbox` config, whose memoized detection means one probe for the whole run): parses ALL files up front (fail fast; nothing runs on a parse error), then runs files concurrently under ONE global pool of N slots bounding every spawned command (test instances AND hook commands). Also home to `slots`, the pool type. Per-file barriers are the same ones `runFile` enforces (setup before any instance, hooks sequential, teardown after the last instance, setup failure fails every instance without running them); output is buffered per file and flushed in canonical order, so the bytes depend only on outcomes. There is NO second per-file implementation to keep in sync — `RunFile` and `RunFiles` both drive `runFile`, which differs only in which pool it is handed
  - `sandbox.go` - Sandboxing: `SandboxMode`/`SandboxConfig` (mode + docker image, where `Image: ""` means the operator named none and a file's `image:` may pick — a typed one outranks the file, and `plan.refusedImage` puts that on the `# sandbox:` line instead of silently swapping images; `Backend()` memoizes detection behind a sync.Once, so the host is probed at most once per process and only when a file needs a sandbox), the probes (all EXERCISE the backend — bwrap is often installed on kernels denying it a userns, the docker CLI often has no daemon), `newSandboxPlan` (CLI choice is the outer bound; a file's spec can only narrow it), and the argv builders. bwrap: `--ro-bind-try` of the OS tool tree (`toolTreePaths`: /usr,/bin,/sbin,/lib*,/etc,/nix,/opt — /opt covers add-on toolchains such as GitHub Actions' hosted tool cache, whose absence loses a workflow's setup-go/-node/-python interpreter; NEVER `/`, which exposed the whole host and made the backends diverge) + the resolv.conf target when it is a symlink outside it + `--dev`/`--proc`/`--tmpfs /tmp` + `--unshare-pid --die-with-parent`, then ro bind of the cwd, then per-file `--bind`s, then exactly ONE `--chdir` (a second makes bwrap warn into the command's captured stderr), `--unshare-net` when network is off (ORDER IS LOAD-BEARING: the tmpfs must precede a work dir under /tmp, and the writable binds follow the cwd bind so a writable path inside it wins). docker: `--rm -i --init --name <n>` + `--user` + rw bind of the work dir (targets deduplicated, rw wins) + ro bind of the cwd + `-e` for the inherited run environment (`inheritedEnv`, minus `imageOwnedEnv`) then dats-added env, ending in `image bash -c cmd`. **The two backends expose the SAME host paths** (cwd ro + declared writable), pinned by `TestBwrapAndDockerExposeTheSameHostPaths`; seatbelt still does not restrict reads (known gap). the ONLY writable paths are the file's temp dir and `--coverdir` (`writablePaths`) -- there is deliberately no `sandbox.writable` key and no `--writable` flag: scratch goes in the temp dir, and a command that needs the host needs a `--no-sandbox` run (that includes a self-rewriting binary such as an APE -- copy it into the temp dir and run it there); the returned `Kill` hook `docker kill`s the container, since killing the client would leave the workload running. Auto order is bwrap -> seatbelt -> docker: the two native backends are platform-exclusive, so this reads as "the native sandbox for this OS, else docker"
  - `sandbox_seatbelt.go` - The macOS backend: `probeSeatbelt` (compiles+applies a real profile, since sandbox-exec's mere presence proves nothing) and the SBPL generator. `sandbox-exec -p <profile> bash -c cmd` (inline, no temp file). The profile is LAST-MATCH-WINS and that order is the policy: `(allow default)` -> `(deny file-write*)` -> `(allow file-write* (subpath ...))`, plus `(deny network*)` when the file cut the network, and the writable device nodes a shell needs (/dev/null, /dev/fd, tty). `seatbeltWritablePaths` resolves symlinks BEFORE writing subpath rules -- macOS matches the real path and dats' temp dirs arrive via /tmp -> /private/tmp, so an unresolved rule matches nothing and every fixture write is denied. Unlike bwrap there is no PID namespace: files and network are confined, the process table is not
  - `exec.go` - Command execution via bash, driven by an `execRequest` (cmd, stdin, env + the dats-added `EnvExtra`, timeout, low-priority, sandbox plan). Execute takes the caller's context as the base (canceling it group-kills the command and reports a signal death; TimedOut keys on the derived per-command context, so parent cancellation — context.Canceled — never counts as a timeout); per-test timeouts layer WithTimeout on top and kill the whole process group, pipes are force-closed ~1s after exit (WaitDelay) so orphans can't block, signal deaths are surfaced (NOTE: bwrap reports its child's signal death as exit 128+N, so a sandboxed signal death shows as an exit code, not a signal; timeouts still report as timeouts). A sandboxed request also gets the backend's `Kill` hook fired (async) from Cancel. A multi-file run (`RunFiles`) additionally renices each spawned command's process group to nice 19 right after start (`setLowPriority`; best-effort, platform-split: unix real / windows no-op); a direct `RunFile` call makes zero priority syscalls
  - `fixtures.go` - Creates input files, validates fixture-name locality, creates parent dirs for nested declared outputs, expands `{inputs.X}`/`{outputs.X}`/`{shared.X}` placeholders; SetupSharedFixtures writes file-level shared files ({shared.X}-only expansion via ExpandSharedPlaceholders). `SetupFixtures`/`SetupSharedFixtures` both take a `sourceDir` (the `.dats` file's own directory, from `Runner.sourceDir`/`sourceDirOf` in runner.go -- resolved once per file, alongside `plan`) and, after writing `files`, copy in every `copy` entry via `copyHostFile` (`resolveSource` joins a relative source against `sourceDir`; preserves the source's permission bits, e.g. a script's executable bit) into the SAME input/shared directory and `{inputs.X}`/`{shared.X}` namespace `files` uses -- a destination collision between the two maps is re-checked here too, for a library caller bypassing ParseFile
  - `snapshot.go` - Snapshot (golden-file) assertions: SnapshotDir (`<file>.snapshots` next to the .dats), GoldenFileName (`NNN-<slug>.<stream>.golden`, NNN = canonical 1-based instance number, slug from the instance display name), NormalizeSnapshotText ({testdir}/{shareddir}/{tmproot} tokens, longest-path-first), applySnapshot (called by RunFile AND runFileParallel after the instance name is set; compares — or under `Runner.Update` rewrites — goldens, only for commands that ran to completion, never updating from an instance with other failures), and pruneStaleGoldens (update mode after a clean setup: removes unexpected `*.golden` files, removes an emptied dir, touches nothing else)
  - `assert.go` - Assertion functions (AssertContains, AssertLineRegex, AssertExitCode, etc.)
  - `output.go` - Result types + TAP-like formatting, including `PrintSandbox` (the `# sandbox: <backend>` line before each file's header; silent when the file ran on the host, so unsandboxed output is byte-identical to before). Result types (TestResult with UpdatedGoldens, FileResult with SetupFailure/TeardownFailures/PrunedGoldens + Ok(), CommandFailure) and TAP-like formatting (PrintHookFailure diagnostics, `# updated golden:`/`# pruned stale golden:` lines, `teardown failed` summary annotation)
- `report/` - Machine-readable report rendering (public, importable by external modules)
  - `junit.go` - `WriteJUnit`: JUnit XML (testsuites/testsuite/testcase; failed instances carry failure + system-out/err; synthetic `[setup]` first / `[teardown]` trailing cases for hook failures, counted in the tests/failures attrs so JUnit totals ≥ CLI counts) + the XML 1.0 control-char sanitizer (illegal runes → U+FFFD)
  - `json.go` - `WriteJSON`: JSON report (`format_version` 1; summary counts = CLI instance counts; hook failures in setup_failure/teardown_failures; stdout/stderr keys present exactly on failed instances). Field names are a stability contract — see `docs/reports.md` before changing anything here
- `docs/` - Additional prose documentation (`library.md` = the Go API and its contracts; `reports.md` = report formats + stability contract); `schema.json` - JSON Schema for IDE validation

### Key Types
- **ExitCode** - Can be int 0-255 (bare or quoted, e.g. `"3"`) or string like `EXIT_SUCCESS`/`EXIT_FAILURE`
- **Duration** - Per-test timeout; int seconds (bare or quoted, e.g. `"5"`) or Go duration string (e.g. `500ms`, `2s`, `1m30s`)
- **OutputCheck** - Either `[]string` (patterns) or `map[int]string` (line-specific regex, 0-indexed; duplicate or negative line keys are parse errors)
- **OutputBlock** - Handles stdout, stderr, !stdout, !stderr, files, !files, snapshot, and json_output checks
- **SnapshotCheck** - The `outputs.snapshot` key: scalar bool (`true` = snapshot stdout; `false` = zero value, same as omitted) or a map of stream booleans (`stdout`/`stderr`, at least one true; duplicate/unknown keys and non-bool values are parse errors). Value type (no pointer) so matrix `copyTest` duplicates it by plain value copy; holds no strings, so it is outside the `{matrix.X}` substitution scope
- **SandboxSpec** - The file-level `sandbox` key: a map of `network`/`image` only (there is deliberately no `writable` key -- scratch goes in the temp dir -- and no `enabled` key: a file narrows its sandbox, never turns it off). The `Network` pointer keeps "unstated" distinct from an explicit `false`; nil spec = nothing narrowed. `NetworkEnabled`/`ImageName` are the nil-safe accessors, and `image:` is a request, not a decision: a typed `--sandbox-image` wins. `sandbox: false`/`enabled:` (both spellings of off) are parse errors naming `--no-sandbox`, and so is `sandbox: true`; unknown/duplicate keys, non-bool `network`, empty `image`, a non-mapping value, and an empty mapping are too. `{matrix.X}` is rejected in `image` (the sandbox is resolved once per file, before instances exist)
- **FileCheck** - Validates output files with `exists`, `match`, and `notMatch` properties; an empty check (`{}` or null) is an implicit existence assertion
- **InputBlock** - Contains `stdin` (string), `files` (map of filename to content), `copy` (map of filename to a host source
  path, copied in writable -- the read-write counterpart of the sandbox's read-only cwd bind mount; a name may not also
  appear under `files`; `{matrix.X}` substitutes into the source), and `env` (map of env var name to value, added to the
  inherited environment in sorted key order). Depth: `docs/file-format.md#copy-fixtures-inputscopy-and-sharedcopy`
- **HookCommand / CommandList / SetupCommands / TeardownCommands** - `HookCommand` is one `setup`/`teardown` entry: `Cmd`, optional `Env`, `StdinFile` (raw content piped to stdin, resolved like `inputs.copy`), and `Timeout` (`*Duration`, nil = `DefaultHookTimeout` 30s via `EffectiveTimeout()`; an explicit value must be > 0 — a hook always has a bound, unlike a test's 0/omitted = unbounded). YAML form is a bare command string, or a mapping (`cmd`, `env`, `stdin_file`, `timeout`; unknown/duplicate keys and a missing `cmd` are parse errors). `CommandList` is `[]HookCommand`; `SetupCommands`/`TeardownCommands` wrap it so parse errors name their key. Empty lists, blank/non-string/non-mapping entries, a shell heredoc (`<<WORD`), and a herestring (`<<<`) in `cmd` are all parse errors
- **Shared** - File-level `shared` block with `Files map[string]string` and `Copy map[string]string` (same read-write-copy
  semantics as `InputBlock.Copy`, resolved once per file; `{matrix.X}` in a source is rejected, no instance exists yet);
  must declare at least one entry across the two, names disjoint and locality-validated (nil pointer on TestFile when absent)
- **Matrix / TestInstance** - Per-test `matrix` block: ordered `[]MatrixVariable` (declaration order is semantic — label order and expansion order, last variable fastest); values are the literal scalar text (`1.50` stays `"1.50"`). `ExpandMatrix` yields `TestInstance`s (deep-copied substituted Test + `[k=v, ...]` label + assignments). Bad names, empty/non-sequence value lists, non-scalar or duplicate values, and undeclared references are parse errors; `matrix:` with explicit null = absent

### Placeholder System
Commands, `inputs.files` contents, and `inputs.env` values use `{inputs.X}`, `{outputs.X}`, and `{shared.X}`, which expand to absolute paths in the temp directory:
- `{inputs.foo.txt}` → `/tmp/dats-xxx/test-N/inputs/foo.txt` (X must be declared under `inputs.files`; otherwise left as-is)
- `{outputs.result.txt}` → `/tmp/dats-xxx/test-N/outputs/result.txt` (no `outputs.files` check required, as long as X is a local relative path; non-local names are left as-is)
- `{shared.config.json}` → `/tmp/dats-xxx/shared/config.json` (file-wide directory shared by all tests; no declaration required, same locality rule as `{outputs.X}`)

Setup commands, teardown commands, and `shared.files` contents expand ONLY `{shared.X}`; `{inputs.X}`/`{outputs.X}` stay verbatim there. `inputs.stdin` is never expanded.

`{matrix.X}` is a separate, earlier layer: single-pass text substitution at instance-expansion time (before any runtime expansion), also reaching `desc`, `inputs.stdin`, `inputs.copy` sources, output patterns, and json_output strings. Matrix values may contain other placeholders (expanded at runtime as usual); substituted text is never re-scanned. Matrix placeholders in setup/teardown/shared/shared.copy are parse errors (`not available outside tests`); fixture file NAMES (files and copy destinations alike) and env var NAMES are never substituted.

Fixture names (`inputs.files`, `inputs.copy`, `outputs.files`, `outputs.!files`, `shared.files`, `shared.copy`) must be local relative paths (no `..`/absolute; enforced at parse time and again at fixture setup), and a name may not appear under both `files` and `copy` in the same block. Nested names like `sub/file.txt` are allowed; parent directories of declared output files and of shared files are auto-created. `inputs.copy`/`shared.copy` sources resolve relative to the `.dats` file's own directory and are copied in writable, preserving permission bits -- see `docs/file-format.md#copy-fixtures-inputscopy-and-sharedcopy`. A heredoc (`<<WORD`) or a herestring (`<<<`) in `cmd`/`setup`/`teardown` is a parse error; use `copy`/`files` or `inputs.stdin`/a pipe instead, respectively.

## DATS File Format

```yaml
sandbox:                    # Optional: narrow this file's sandbox (never turn it off)
	network: false            # (or image: alpine:3.20)
shared:                     # Optional file-level fixtures (once per file)
	files:
		config.json: content    # written into shared/, addressed as {shared.config.json}
	copy:
		real.bin: fixtures/real.bin   # copied into shared/, writable (path relative to this .dats file)
setup: single command       # Optional; or a list of command strings
teardown:                   # Optional; ALWAYS runs (even after setup failure)
	- first cleanup command
	- second cleanup command
tests:
	- desc: optional description
	  cmd: command to run       # Required, supports {inputs.X} and {outputs.X}
	  exit: 0                   # Optional, default 0 (or EXIT_SUCCESS/EXIT_FAILURE)
	  timeout: 2s               # Optional, int seconds or Go duration string; 0/omitted = no timeout
	  matrix:                   # Optional; expands the test into one instance per combination
		greeting: [hello, howdy]  # values referenced as {matrix.greeting}
	  inputs:
		stdin: "input text"     # Optional, piped to cmd
		files:                  # Optional, creates fixture files
			file.txt: content
		copy:                   # Optional, copies a host file in, writable
			real.bin: fixtures/real.bin   # (path is relative to this .dats file)
		env:                    # Optional, env vars added to the inherited environment
			MY_VAR: value         # (values support {inputs.X}/{outputs.X} placeholders)
	  outputs:                  # Optional
		stdout:                 # Pattern list (substring match)...
			- "pattern"
		# ...or a line-number map instead (0-indexed regex), not both:
		# stdout:
		#   0: "^first line$"
		#   2: "^third line$"
		!stdout:              # Patterns that must NOT appear (also accepts the line-number map form)
			- "error"
		stderr:
			- "warning"
		files:                  # Output file validation
			result.txt:
				exists: true
				match:
					- "expected content"
				notMatch:
					- "error"
		!files:               # Negated output file validation (each check inverted)
			unexpected.txt:
				exists: true        # must NOT exist
		snapshot: true          # Golden-file assertion: stdout must byte-match
			# <file>.snapshots/NNN-<slug>.stdout.golden
			# (or {stdout: bool, stderr: bool}; --update rewrites)
```

### File-Level Properties

| Property | Required | Description |
|----------|----------|-------------|
| `shared.files` | No | Map of filename → content, written once per file into `shared/` before setup; contents expand `{shared.X}` only; names must be local relative paths |
| `setup` | No | Hook command or list (bare string, or a mapping of `cmd`/`env`/`stdin_file`/`timeout`), run once in order before the file's tests; `cmd`/`env` expand `{shared.X}` only, bounded by `timeout` (default 30s, must be > 0). A failure skips remaining setup commands and reports EVERY test as failed (reason `file setup failed`, never "skipped"); teardown still runs |
| `sandbox` | No | A mapping (`network`, `image`) that narrows the sandbox for this file's commands (tests AND setup/teardown). File-level only -- one file's commands share one temp dir and one hook lifecycle. The CLI is the outer bound: under `--no-sandbox` the block is inert, and nothing in a file can turn its own sandbox off (`sandbox: false` is a parse error pointing at `--no-sandbox`) |
| `teardown` | No | Same hook command or list form as `setup`, always run once in order after the file's tests (after failures, even after setup failure; one failure does not stop the rest). Any failure marks the file failed (exit 1) even when all tests passed |

### Test Properties

| Property | Required | Description |
|----------|----------|-------------|
| `cmd` | Yes | Command to run. Use `{inputs.X}`, `{outputs.X}`, and `{shared.X}` for file paths |
| `desc` | No | Description for the test (used in output) |
| `exit` | No | Expected exit code (default: 0). Int 0-255 (bare or quoted, e.g. `"3"`) or `EXIT_SUCCESS`/`EXIT_FAILURE`; floats rejected at parse time |
| `timeout` | No | Per-test timeout: int seconds (bare or quoted, e.g. `"5"`) or Go duration string (e.g. `500ms`, `2s`). 0/omitted = no timeout; floats rejected (write `1.5s`, not `1.5`) |
| `matrix` | No | Map of variable name → list of scalar values; expands the test into one instance per combination (cartesian product, declaration order, last variable varies fastest). `{matrix.X}` substitutes in desc, cmd, stdin, file contents, env values, and output patterns; every instance always runs, reported as `desc [k=v, ...]` |
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

## CI/CD

GitHub Actions workflow (`.github/workflows/ci.yml`) runs on push with two jobs:
- `test` - installs bubblewrap, clears ubuntu-24.04's `kernel.apparmor_restrict_unprivileged_userns` (which otherwise denies bwrap the user namespace it needs, silently turning every bwrap test into a skip) and runs the bwrap probe as its own step so an unusable backend fails with its own error; then builds the Go binary (multi-platform), runs tests via `wow-look-at-my/go-toolchain`, and creates releases on master pushes. The sandbox integration tests skip themselves when no backend is usable, so that probe step -- not any env knob -- is what stops a skip from passing for isolation coverage in CI; the docker tests use the runner's own daemon and skip if the image cannot be fetched (a registry outage says nothing about the code). `artifact-metadata: write` is required by the publish step (job-level permissions REPLACE workflow-level ones)
- `schema` - validates `testdata/schema/*.json` fixtures against `schema.json` using the `wow-look-at-my/json-validator` action, guarding against schema drift

## Consuming dats from another repo's CI

`action.yml` at repo root makes this a composite GitHub Action:
`uses: wow-look-at-my/dats@master` downloads the newest build from buildhost
(never pinned) and runs it via one `args:` input — see the README's
"GitHub Actions" section. It wraps
`wow-look-at-my/buildhost/.github/actions/buildhost-download` (`project:
dats`), the same download every consumer used to hand-roll with curl/chmod.
On Linux it also installs bubblewrap and, if it's blocked, clears Ubuntu
24.04's default `apparmor_restrict_unprivileged_userns` restriction the same
way this repo's own CI does (see "CI/CD" above) — so a caller gets real
sandboxing without needing `--no-sandbox` to work around the runner.

## JSON Schema

`schema.json` provides IDE validation for `.dats` files. Can be used with YAML language servers.
