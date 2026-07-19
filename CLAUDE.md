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

# Keep temp directory for debugging
./build/dats --keep-temp examples/example.dats
```

## Architecture

### Core Flow
1. `.dats` YAML file is parsed using `gopkg.in/yaml.v3`
2. Every test is expanded up front into its matrix instances (`schema.ExpandMatrix`; non-matrix tests = one instance) — the header count, instance numbering, temp dirs, summary counts, and setup-failure reporting all operate on the expanded list; every instance always runs (no test filtering/selection by design)
3. Per file: a `shared/` dir is created, `shared.files` are written into it, and `setup` commands run in order (a failure fails EVERY test instance in the file — reported as failures, never "skipped" — but teardown still runs)
4. For each test instance, fixtures are set up in a temp directory
5. Command is executed via `bash -c` with placeholder expansion
6. Exit code, stdout, stderr, and output files are validated against assertions; `outputs.snapshot` additionally byte-compares captured streams against golden files in `<file>.snapshots/` next to the .dats file (temp paths normalized to `{testdir}`/`{shareddir}`/`{tmproot}` tokens), and `--update` rewrites those goldens from actual output (never from an instance with other failures) and prunes stale ones
7. `teardown` commands always run in order (after test failures and even when setup failed); any teardown failure marks the file failed (exit 1) even when all tests passed
8. Results are printed in TAP-like format
9. With `-j`/`--jobs` (jobs mode) the same per-file semantics run concurrently: one global N-slot pool bounds every spawned command (instances and hooks) across all files, per-file barriers are preserved, spawned commands are reniced to 19 (unix, best-effort), and output is buffered and printed in canonical order — byte-identical to a serial run when outcomes are equal. Flag absent = the serial path above, untouched
10. With `--report-junit`/`--report-json`, runTests writes report files from the finished results at end of run — always when the run executed (especially failing runs; identical data serial and `-j`), never on hard errors that abort the run; a report write failure is itself an error (stderr, exit 1). Formats and stability contract: `docs/reports.md`

### Go Package Structure
- `main.go` - Minimal entry point; calls `cmd.Execute()`
- `cmd/` - Cobra CLI commands (each command self-registers in its own file)
  - `root.go` - Root command and persistent flags (`--verbose`, `--keep-temp`, `--coverdir`, `-j`/`--jobs`, `--report-junit`/`--report-json`, `--update`); failing runs exit 1 without usage dumps, errors print exactly once
  - `jobs.go` - The `-j`/`--jobs` flag: registration (int flag with NoOptDefVal = NumCPU so bare `-j` works), make-style `-jN` argv normalization to `--jobs=N` (pflag resolves NoOptDefVal before the attached `-farg` form, so a raw `-j4` would fail; space-separated `-j 4` intentionally leaves `4` positional, as in GNU make), and resolution (absent → 0 = serial; explicit N < 1 → error)
  - `report.go` - The `--report-junit`/`--report-json` flags (long-only, value required) and the write-to-disk plumbing (MkdirAll parent dirs, attempt both files, errors.Join); rendering lives in the `report` package. runTests measures the execution wall time and calls writeReports after totals — always when the run executed, never on hard errors that abort it
  - `update.go` - The `--update` flag (long-only bool): rewrite snapshot golden files instead of failing. Plumbed into `Runner.Update` by runTests, which also prints the end-of-run goldens summary (`Updated N golden file(s)[, pruned M stale]`, silent when nothing changed); `dats syntax` accepts the flag but ignores it
  - `test.go` - `test` subcommand (also the default action): runs tests; jobs==0 runs the serial loop, jobs>=1 calls `runner.RunFilesParallel`; after totals (and the goldens summary under `--update`) it writes any requested report files (a write failure is a real error even when tests passed)
  - `syntax.go` - `syntax` subcommand: validates `.dats` files without running them
  - `version.go` - `version` subcommand and `--version` flag: one-line `dats <version>` from build info
  - `find.go` - Resolves file/directory args (dirs recurse; symlinked dir roots followed) or discovers `.dats` files in the tree; skips hidden dirs/files, dedupes by absolute path
- `schema/` - YAML schema types + parser (public, importable by external modules)
  - `types.go` - Schema types with custom unmarshalers
  - `parse.go` - `ParseFile`: reads and validates a `.dats` file (rejects unknown keys, multi-document YAML, non-local fixture names, undeclared `{matrix.X}` references, and matrix placeholders in setup/teardown/shared)
  - `matrix.go` - `Matrix` (declaration-ordered variables, strict value validation), `ExpandMatrix` (cartesian instance expansion, deep copies, single-pass `{matrix.X}` substitution), and the single definition of the matrix substitution scope shared by validation and expansion
- `runner/` - Native test runner (public, importable by external modules)
  - `runner.go` - Orchestrates test execution (RunFile, RunTest); RunFile also writes shared fixtures, runs setup (stops at first failure; then every test is reported failed with reason "file setup failed"), and always runs all teardown commands (runHookCommand executes one setup/teardown command via the same bash path and env construction as test commands — including `GOCOVERDIR` under `--coverdir` — with {shared.X}-only expansion)
  - `parallel.go` - Jobs-mode orchestration (`RunFilesParallel`): parses ALL files up front (fail fast; nothing runs on a parse error), then runs files concurrently under ONE global pool of N slots bounding every spawned command (test instances AND hook commands). Per-file barriers match serial exactly (setup before any instance, hooks sequential, teardown after the last instance, setup failure fails every instance without running them); output is buffered per file and flushed in canonical order — byte-identical to a serial run when outcomes are equal. `runFileParallel` mirrors RunFile step for step; keep the two in sync
  - `exec.go` - Command execution via bash; per-test timeouts kill the whole process group, pipes are force-closed ~1s after exit (WaitDelay) so orphans can't block, signal deaths are surfaced. Jobs mode additionally renices each spawned command's process group to nice 19 right after start (`setLowPriority`; best-effort, platform-split: unix real / windows no-op); serial runs make zero priority syscalls
  - `fixtures.go` - Creates input files, validates fixture-name locality, creates parent dirs for nested declared outputs, expands `{inputs.X}`/`{outputs.X}`/`{shared.X}` placeholders; SetupSharedFixtures writes file-level shared files ({shared.X}-only expansion via ExpandSharedPlaceholders)
  - `snapshot.go` - Snapshot (golden-file) assertions: SnapshotDir (`<file>.snapshots` next to the .dats), GoldenFileName (`NNN-<slug>.<stream>.golden`, NNN = canonical 1-based instance number, slug from the instance display name), NormalizeSnapshotText ({testdir}/{shareddir}/{tmproot} tokens, longest-path-first), applySnapshot (called by RunFile AND runFileParallel after the instance name is set; compares — or under `Runner.Update` rewrites — goldens, only for commands that ran to completion, never updating from an instance with other failures), and pruneStaleGoldens (update mode after a clean setup: removes unexpected `*.golden` files, removes an emptied dir, touches nothing else)
  - `assert.go` - Assertion functions (AssertContains, AssertLineRegex, AssertExitCode, etc.)
  - `output.go` - Result types (TestResult with UpdatedGoldens, FileResult with SetupFailure/TeardownFailures/PrunedGoldens + Ok(), CommandFailure) and TAP-like formatting (PrintHookFailure diagnostics, `# updated golden:`/`# pruned stale golden:` lines, `teardown failed` summary annotation)
- `report/` - Machine-readable report rendering (public, importable by external modules)
  - `junit.go` - `WriteJUnit`: JUnit XML (testsuites/testsuite/testcase; failed instances carry failure + system-out/err; synthetic `[setup]` first / `[teardown]` trailing cases for hook failures, counted in the tests/failures attrs so JUnit totals ≥ CLI counts) + the XML 1.0 control-char sanitizer (illegal runes → U+FFFD)
  - `json.go` - `WriteJSON`: JSON report (`format_version` 1; summary counts = CLI instance counts; hook failures in setup_failure/teardown_failures; stdout/stderr keys present exactly on failed instances). Field names are a stability contract — see `docs/reports.md` before changing anything here
- `docs/` - Additional prose documentation (`reports.md` = report formats + stability contract); `schema.json` - JSON Schema for IDE validation

### Key Types
- **ExitCode** - Can be int 0-255 (bare or quoted, e.g. `"3"`) or string like `EXIT_SUCCESS`/`EXIT_FAILURE`
- **Duration** - Per-test timeout; int seconds (bare or quoted, e.g. `"5"`) or Go duration string (e.g. `500ms`, `2s`, `1m30s`)
- **OutputCheck** - Either `[]string` (patterns) or `map[int]string` (line-specific regex, 0-indexed; duplicate or negative line keys are parse errors)
- **OutputBlock** - Handles stdout, stderr, !stdout, !stderr, files, !files, snapshot, and json_output checks
- **SnapshotCheck** - The `outputs.snapshot` key: scalar bool (`true` = snapshot stdout; `false` = zero value, same as omitted) or a map of stream booleans (`stdout`/`stderr`, at least one true; duplicate/unknown keys and non-bool values are parse errors). Value type (no pointer) so matrix `copyTest` duplicates it by plain value copy; holds no strings, so it is outside the `{matrix.X}` substitution scope
- **FileCheck** - Validates output files with `exists`, `match`, and `notMatch` properties; an empty check (`{}` or null) is an implicit existence assertion
- **InputBlock** - Contains `stdin` (string), `files` (map of filename to content), and `env` (map of env var name to value, added to the inherited environment in sorted key order)
- **CommandList / SetupCommands / TeardownCommands** - File-level `setup`/`teardown` values: a single command string or a sequence of command strings ([]string underneath); the two wrapper types exist so parse errors name their key. Empty lists, blank commands, and non-string entries are parse errors
- **Shared** - File-level `shared` block with `Files map[string]string`; must declare at least one file, names get the same locality validation as `inputs.files` (nil pointer on TestFile when absent)
- **Matrix / TestInstance** - Per-test `matrix` block: ordered `[]MatrixVariable` (declaration order is semantic — label order and expansion order, last variable fastest); values are the literal scalar text (`1.50` stays `"1.50"`). `ExpandMatrix` yields `TestInstance`s (deep-copied substituted Test + `[k=v, ...]` label + assignments). Bad names, empty/non-sequence value lists, non-scalar or duplicate values, and undeclared references are parse errors; `matrix:` with explicit null = absent

### Placeholder System
Commands, `inputs.files` contents, and `inputs.env` values use `{inputs.X}`, `{outputs.X}`, and `{shared.X}`, which expand to absolute paths in the temp directory:
- `{inputs.foo.txt}` → `/tmp/dats-xxx/test-N/inputs/foo.txt` (X must be declared under `inputs.files`; otherwise left as-is)
- `{outputs.result.txt}` → `/tmp/dats-xxx/test-N/outputs/result.txt` (no `outputs.files` check required, as long as X is a local relative path; non-local names are left as-is)
- `{shared.config.json}` → `/tmp/dats-xxx/shared/config.json` (file-wide directory shared by all tests; no declaration required, same locality rule as `{outputs.X}`)

Setup commands, teardown commands, and `shared.files` contents expand ONLY `{shared.X}`; `{inputs.X}`/`{outputs.X}` stay verbatim there. `inputs.stdin` is never expanded.

`{matrix.X}` is a separate, earlier layer: single-pass text substitution at instance-expansion time (before any runtime expansion), also reaching `desc`, `inputs.stdin`, output patterns, and json_output strings. Matrix values may contain other placeholders (expanded at runtime as usual); substituted text is never re-scanned. Matrix placeholders in setup/teardown/shared are parse errors (`not available outside tests`); fixture file NAMES and env var NAMES are never substituted.

Fixture names (`inputs.files`, `outputs.files`, `outputs.!files`, `shared.files`) must be local relative paths (no `..`/absolute; enforced at parse time and again at fixture setup). Nested names like `sub/file.txt` are allowed; parent directories of declared output files and of shared files are auto-created.

## DATS File Format

```yaml
shared:                     # Optional file-level fixtures (once per file)
  files:
    config.json: content    # written into shared/, addressed as {shared.config.json}
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
      env:                    # Optional, env vars added to the inherited environment
        MY_VAR: value         # (values support {inputs.X}/{outputs.X} placeholders)
    outputs:                  # Optional
      stdout:                 # Pattern list or line-number map
        - "pattern"           # Substring match
      stdout:                 # Or use line-specific regex (0-indexed)
        0: "^first line$"
        2: "^third line$"
      "!stdout":              # Patterns that must NOT appear (also accepts the line-number map form)
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
      "!files":               # Negated output file validation (each check inverted)
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
| `setup` | No | Command string or list, run once in order before the file's tests. Only `{shared.X}` expands. A failure skips remaining setup commands and reports EVERY test as failed (reason `file setup failed`, never "skipped"); teardown still runs |
| `teardown` | No | Command string or list, always run once in order after the file's tests (after failures, even after setup failure; one failure does not stop the rest). Any failure marks the file failed (exit 1) even when all tests passed |

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
- `test` - builds the Go binary (multi-platform), runs tests via `wow-look-at-my/go-toolchain`, and creates releases on master pushes
- `schema` - validates `testdata/schema/*.json` fixtures against `schema.json` using the `wow-look-at-my/json-validator` action, guarding against schema drift

## JSON Schema

`schema.json` provides IDE validation for `.dats` files. Can be used with YAML language servers.
