# DATS - Declarative Automated Testing System

A Go CLI that runs tests defined in declarative YAML files (`.dats`). It natively executes commands, captures output, and verifies assertions without requiring external test frameworks.

Test commands are **sandboxed by default** (bubblewrap on Linux, `sandbox-exec` on macOS, falling back to docker): writes are confined to the test's temp directory, and running on the host is an explicit opt-out. A `.dats` file can narrow its own sandbox but never switch it off. See [docs/cli.md](docs/cli.md#sandboxing---sandbox).

Go programs can skip the binary entirely and link the runner: `dats.Run(ctx, dats.Options{...})` runs suites in-process, with the same behavior and output as the CLI. See [docs/library.md](docs/library.md).

The whole reference under [docs/](docs/README.md) is compiled into the binary, so a machine with `dats` and nothing else still has it: `dats docs` lists the topics.

## Installation

```bash
just build          # Build the dats binary to build/dats
just install        # Symlink binary to ~/.local/bin/dats
```

### GitHub Actions

`wow-look-at-my/dats@master` is a composite action: it downloads the newest `dats` build from buildhost (no pinned version to drift behind) and runs it. So a workflow does not have to hand-roll the download. On Linux it also makes sure bubblewrap sandboxing actually works first (installing it if missing, and clearing Ubuntu 24.04's default AppArmor restriction on unprivileged user.

```yaml
- uses: wow-look-at-my/dats@master
  with:
    tests: tests/
```

| Input | Required | Default | Description |
|-------|----------|---------|-------------|
| `tests` | Yes | — | Space- or newline-separated `.dats` files and directories to run. A directory runs its top-level `*.dats` files. Paths are relative to `working-directory`. |
| `working-directory` | No | `.` | Directory to run `dats` from |
| `jobs` | No | One per CPU | Commands to run at once, as `-j` takes it. `1` runs one at a time, which a stateful suite needs. |
| `sandbox` | No | `auto` | `auto`, `bwrap`, `seatbelt`, `docker` or `none`. `none` is for a suite that drives the host itself, and skips the backend install. |

Every input is typed and checked: there is no way to pass arbitrary flags. And a value dats will not accept fails in the action naming the input. Outputs `path`, the full path to the downloaded binary.

## Usage

```bash
# Run test files (positional args or via 'test' subcommand)
dats tests.dats
dats test tests.dats

# Run all .dats files in a directory (recursively)
dats test tests/

# Run all .dats files in the current directory tree
dats test

# Verbose mode (shows command details, full output on failure)
dats -v test tests.dats

# Parallel execution: 4 workers, or one per CPU with bare -j
# (attach the value: -j4/-j=4/--jobs=4 — "-j 4" does NOT bind the 4)
dats -j4 test tests/
dats -j test tests/

# Machine-readable reports for CI/editors (written even when the run fails)
dats --report-junit report.xml test tests/
dats --report-json report.json test tests/

# Rewrite snapshot golden files from actual output (see outputs.snapshot)
dats --update test tests/

# Watch mode: run everything, then re-run the complete scope on file changes
dats watch tests/

# Run commands on the host instead of in a sandbox (sandboxing is the default)
dats --no-sandbox test.dats

# Keep temp directory for debugging
dats test --keep-temp tests.dats

# Validate .dats file syntax without running tests
dats syntax tests.dats

# Validate all .dats files in current directory tree
dats syntax

# Print the version
dats version

# The documentation ships in the binary: topics, one page, or all of it
dats docs
dats docs format
dats help format
```

Both `test` and `syntax` accept any mix of `.dats` files and directories. Directory arguments and no-arg discovery recurse the tree, skipping hidden directories and hidden `.dats` files (leading `.`). Explicitly named files are always accepted. Repeated arguments are deduplicated by absolute path.

### Subcommands

| Command | Description |
|---------|-------------|
| `test` | Run tests from `.dats` files or directories (default when no subcommand given) |
| `watch` | Run tests, then re-run the **complete** argument scope whenever the resolved `.dats` files, their `.snapshots/` golden dirs, or directory arguments change (debounced; no per-file re-run — dats has no test filtering by design). Ctrl-C exits 0. See [docs/cli.md](docs/cli.md#watch-mode-dats-watch) |
| `syntax` | Validate `.dats` file syntax without executing tests |
| `docs` | Print the documentation compiled into the binary: `dats docs` lists the topics, `dats docs format` prints the file-format reference, `dats docs all` prints everything |
| `version` | Print a one-line `dats <version>` |

### Flags

| Flag | Scope | Description |
|------|-------|-------------|
| `-v, --verbose` | Global | Show verbose output |
| `-j, --jobs[=N]` | Global | Run up to N test commands concurrently. **Default (flag absent) is one per logical CPU**; use `-j1` for one command at a time. Attach the value: `-jN`/`-j=N`/`--jobs=N` — a space-separated `-j N` leaves `4` positional, as in GNU make. Spawned commands run at low OS priority (unix nice 19, best-effort). Output never depends on this: results are buffered per file and printed in canonical order, so any `-j` produces identical bytes for identical outcomes; see [docs/cli.md](docs/cli.md) |
| `--report-junit <path>` | Global | Write a JUnit XML report of the run to `<path>` — also (especially) on failing runs; identical data in serial and `-j` runs. See [docs/reports.md](docs/reports.md) |
| `--report-json <path>` | Global | Write a JSON report of the run to `<path>` (`format_version` 1, a stable consumption contract). See [docs/reports.md](docs/reports.md) |
| `--update` | Global | Rewrite snapshot golden files (`outputs.snapshot`) from actual output instead of failing, pruning stale ones; every write/prune is listed. See [docs/cli.md](docs/cli.md#updating-snapshots---update) |
| `--sandbox <mode>` | Global | Sandbox backend for test commands: `auto` (default — bwrap, then seatbelt, then docker), `bwrap`, `seatbelt`, `docker`, `none`. See [docs/cli.md](docs/cli.md#sandboxing---sandbox) |
| `--no-sandbox` | Global | Run test commands directly on the host (same as `--sandbox=none`) |
| `--sandbox-image <ref>` | Global | Image the docker backend runs commands in (default `debian:stable-slim`); typed, it pins the run and outranks a file's `image:` |
| `--ssh <[user@]host>` | Global | Run every test command on another machine over ssh. Replaces the sandbox rather than nesting in one, and each file's header line says so. See [docs/cli.md](docs/cli.md#remote-execution---ssh) |
| `--keep-temp` | Global | Keep temp directory for debugging |
| `--coverdir` | Global | Set GOCOVERDIR on executed commands (tests and file-level setup/teardown) to collect coverage data |
| `--version` | Root | Print `dats <version>` (same output as `dats version`) |

## DATS File Format

`.dats` files are indented with tabs, not spaces, and have no anchors/aliases — see [YAML Dialect](docs/file-format.md#yaml-dialect).

```yaml
# Optional file-level keys: shared fixture files written once per file,
# setup command(s) run once before the tests, and teardown command(s) that
# always run once after them (string or list form each).
shared:
	files:
		config.json: |
			{"debug": true}
setup: cat {shared.config.json} > {shared.generated.txt}
teardown:
	- echo cleanup one
	- echo cleanup two

tests:
	# Simple command
	- desc: echo test
	  cmd: echo Hello World
	  outputs:
		stdout:
			- "Hello World"

	# Shared fixtures are addressed with {shared.X} (read-only by convention)
	- desc: reads a shared fixture
	  cmd: cat {shared.generated.txt}
	  outputs:
		stdout:
			- '"debug": true'

	# Command with input file
	- desc: cat reads file
	  inputs:
		files:
			input.txt: |
				Hello, world!
	  cmd: cat {inputs.input.txt}
	  outputs:
		stdout:
			- "Hello, world!"

	# Pulling in an existing host file, writable -- the read-write counterpart
	# of the sandbox's read-only bind mount of the working directory. The
	# source resolves relative to this .dats file. If you want a file, write
	# the file and copy or bind mount it in -- never a heredoc (see below).
	- desc: modifies a copied-in fixture
	  inputs:
		copy:
			config.json: fixtures/config.json
	  cmd: echo "patched" >> {inputs.config.json}; cat {inputs.config.json}
	  outputs:
		stdout:
			- "patched"

	# Command with stdin
	- desc: cat reads stdin
	  inputs:
		stdin: "Hello from stdin"
	  cmd: cat
	  outputs:
		stdout:
			- "Hello from stdin"

	# Per-test environment variables (added to the inherited environment;
	# values go through placeholder expansion)
	- desc: env var visible to command
	  inputs:
		env:
			MY_VAR: hello
	  cmd: echo "$MY_VAR"
	  outputs:
		stdout:
			- "hello"

	# Expected non-zero exit
	- desc: grep returns 1 when not found
	  exit: 1
	  inputs:
		stdin: "hello world"
	  cmd: grep -q "notfound"

	# Line-specific assertions (0-indexed)
	- desc: line matching
	  cmd: printf "line0\nline1\nline2"
	  outputs:
		stdout:
			0: "^line0$"
			2: "^line2$"

	# Negative assertions
	- desc: no errors
	  cmd: echo success
	  outputs:
		stdout:
			- "success"
		!stdout:
			- "error"
			- "fail"

	# Output file validation
	- desc: creates output file
	  cmd: echo "result" > {outputs.result.txt}
	  outputs:
		files:
			result.txt:
				exists: true
				match:
					- "result"
				notMatch:
					- "error"

	# Negated output file (each check inverted: exists true = must NOT exist)
	- desc: no stray file
	  cmd: echo nothing
	  outputs:
		!files:
			unexpected.txt:
				exists: true

	# Per-test timeout (integer seconds or a Go duration string)
	- desc: must finish quickly
	  cmd: echo fast
	  timeout: 2s
	  outputs:
		stdout:
			- "fast"

	# Exit code variables
	- desc: exit code variable
	  exit: EXIT_SUCCESS
	  cmd: true

	# Snapshot (golden-file) assertion: stdout must byte-match the golden
	# stored next to this file (<file>.snapshots/NNN-<slug>.stdout.golden);
	# create/refresh goldens with `dats --update`
	- desc: matches the stored golden
	  cmd: echo stable output
	  outputs:
		snapshot: true

	# Matrix (parameterized) test: one instance per combination (this one runs
	# 4 times, reported as "greets [greeting=hello, name=alice]" and so on)
	- desc: greets
	  cmd: echo "{matrix.greeting}, {matrix.name}!"
	  matrix:
		greeting: [hello, howdy]
		name: [alice, bob]
	  outputs:
		stdout:
			- "{matrix.greeting}, {matrix.name}!"
```

### File-Level Properties

| Property | Required | Description |
|----------|----------|-------------|
| `shared.files` | No | Map of filename → content, written once per file into a `shared/` directory before `setup` runs; addressed via `{shared.X}` placeholders (treat as read-only from tests). Contents expand `{shared.X}` only |
| `shared.copy` | No | Map of filename → host source path, copied once per file into `shared/`, writable. A name may not also appear under `files`; the block needs at least one entry across the two. See [docs/file-format.md](docs/file-format.md#copy-fixtures-inputscopy-and-sharedcopy) |
| `setup` | No | Hook command or list (bare string, or a mapping of `cmd`/`env`/`stdin_file`/`timeout`) run once, in order, before the file's tests. `cmd`/`env` expand `{shared.X}` only; bounded by `timeout` (default 30s, must be > 0 when set). On failure the remaining setup commands are skipped and EVERY test in the file is reported as failed (never "skipped"); teardown still runs |
| `teardown` | No | Same hook command or list form as `setup`, always run once, in order, after the file's tests — after test failures and even when setup failed. One failing command does not stop the rest, but any failure marks the file failed (exit 1) even when all tests passed |
| `sandbox` | No | A mapping (`network`, `image`) narrowing the sandbox for this file's commands — the tests AND the setup/teardown hooks. A file can never turn its own sandbox off: `sandbox: false` and `enabled` are parse errors naming `--no-sandbox`. See [docs/file-format.md](docs/file-format.md#sandbox) |
| `ssh` | No | `[user@]host` this file's commands run on. A request, not a decision: the (file, host) pair must be approved (`dats trust add`), and a typed `--ssh` outranks it. See [docs/file-format.md](docs/file-format.md#ssh) |

Setup and teardown are per-file barriers: parallel mode (`-j`) runs tests concurrently within and across files, but no test in a file starts before. Setup/teardown of different files may overlap in parallel mode — do not assume exclusive access to global resources.

### Test Properties

| Property | Required | Description |
|----------|----------|-------------|
| `cmd` | Yes | Command to run. Use `{inputs.X}`, `{outputs.X}`, and `{shared.X}` for file paths. A shell heredoc (`<<WORD`) is rejected at parse time — write the file and pull it in with `inputs.files`/`inputs.copy` or `shared.files`/`shared.copy` instead — and so is a herestring (`<<<`) — use `inputs.stdin` (or a pipe) instead |
| `desc` | No | Description for the test (used in output) |
| `exit` | No | Expected exit code (default: 0). Int 0-255 (bare or quoted, e.g. `"3"`) or `EXIT_SUCCESS`/`EXIT_FAILURE`; floats are rejected at parse time |
| `timeout` | No | Per-test timeout: integer seconds (bare or quoted, e.g. `"5"`) or a Go duration string (e.g. `500ms`, `2s`, `1m30s`). 0/omitted = no timeout; floats are rejected (write `1.5s`, not `1.5`) |
| `matrix` | No | Map of variable name → list of scalar values; expands the test into one instance per combination (cartesian product, declaration order, last variable varies fastest). Reference values as `{matrix.X}` in desc, cmd, stdin, file contents, env values, and output patterns; every instance always runs and reports as `desc [k=v, ...]` |
| `inputs.stdin` | No | Content piped to command's stdin |
| `inputs.files` | No | Map of filename → content (creates fixture files) |
| `inputs.copy` | No | Map of filename → host source path, copied in writable before running (relative sources resolve against the `.dats` file's directory). A name may not also appear under `files`. See [docs/file-format.md](docs/file-format.md#copy-fixtures-inputscopy-and-sharedcopy) |
| `inputs.env` | No | Map of env var name → value, added to the inherited environment (values go through placeholder expansion) |
| `outputs.stdout` | No | Patterns to match in stdout |
| `outputs.stderr` | No | Patterns to match in stderr |
| `outputs.!stdout` | No | Patterns that must NOT appear in stdout |
| `outputs.!stderr` | No | Patterns that must NOT appear in stderr |
| `outputs.files` | No | Map of filename → FileCheck for output file validation; an empty check (`{}` or null) means "must exist" |
| `outputs.!files` | No | Map of filename → FileCheck with each check inverted (e.g. `exists: true` = must NOT exist; empty check = must NOT exist) |
| `outputs.snapshot` | No | Golden-file assertion: `true` (snapshot stdout) or `{stdout: bool, stderr: bool}` (at least one true). Output must byte-match `<file>.snapshots/NNN-<slug>.<stream>.golden` after temp paths normalize to `{testdir}`/`{shareddir}`/`{tmproot}`; `dats --update` (re)writes goldens and prunes stale ones |
| `outputs.json_output` | No | Expected JSON value of the whole stdout (deep equality) |

File names under `inputs.files`, `inputs.copy`, `outputs.files`, and `outputs.!files` must be relative paths that stay inside the test directory (no `..` or absolute paths. Rejected at parse time, so `dats syntax` catches it). Nested names like `sub/file.txt` are allowed. A name may appear under `files` or `copy`, never both.

### Pulling files into the sandbox

`inputs.files`/`shared.files` author a fixture's content inline as YAML text. `inputs.copy`/`shared.copy` instead copy an *existing* host file in, writable — the read-write counterpart of the sandbox's read-only bind mount of the working directory. Heredocs (`<<WORD`) and herestrings (`<<<`) are both rejected at parse time in `cmd`, `setup`, and `teardown`: a heredoc embeds a file inline instead of using. See [docs/file-format.md](docs/file-format.md#copy-fixtures-inputscopy-and-sharedcopy) for the full reference.

### Output Assertions

- `stdout` / `stderr` - List of patterns to match (substring), or map of line numbers (0-indexed) to regex patterns
- `!stdout` / `!stderr` - Patterns that must NOT appear in output (list of substrings, or map of 0-indexed line numbers to regexes that must not match within that line)
- `files` - Map of output filename to FileCheck with `exists` (bool), `match` (regex patterns that must match), and `notMatch` (regex patterns that must not match). An empty check is an implicit "must exist"
- `!files` - Same FileCheck shape with each check inverted: `exists` flipped, `match` patterns must NOT match, `notMatch` patterns must match. An empty check is an implicit "must NOT exist"
- `snapshot` - Golden-file assertion: captured stdout (and/or stderr) must byte-match a golden stored in a `.snapshots` directory next to the `.dats` file, temp paths normalized to stable. `dats --update` rewrites goldens from actual output and prunes stale ones. See [docs/file-format.md](docs/file-format.md#snapshot-assertions-outputssnapshot)
- `json_output` - Expected JSON value of the whole stdout: keys order-insensitive, arrays order-sensitive, numbers by value

### Failure Reporting

- A failing file-level `setup` command prints a loud file-level diagnostic (command, exit status, captured output) and reports EVERY test in the file as a failure with reason `file setup failed` — never. Teardown still runs.
- A failing file-level `teardown` command prints the same style of diagnostic and marks the file failed (exit 1) even when all tests passed. The summary line gains a `teardown failed` annotation.
- A command that exceeds its `timeout` has its whole process group killed (background children included) and fails with only `command timed out after X` — all other assertions are skipped, since checking. Captured stdout/stderr are still shown in verbose mode.
- A command killed by a signal names it in the exit-code failure, e.g. `expected exit code 0, got -1 (killed by signal: killed)`.
- Multiple failing file checks report in sorted-by-name order.
- Commands that leave orphaned background processes holding stdout/stderr do not block the runner: the pipes are force-closed about 1 second after the main process exits.

### Placeholder System

Commands, `inputs.files` contents, and `inputs.env` values use `{inputs.X}`, `{outputs.X}`, and `{shared.X}`, which expand to absolute paths in a temp directory:
- `{inputs.foo.txt}` → `/tmp/dats-xxx/test-N/inputs/foo.txt` (X must be declared under `inputs.files`. Otherwise left as-is)
- `{outputs.result.txt}` → `/tmp/dats-xxx/test-N/outputs/result.txt` (no `files` check required, as long as X is a local relative path. Non-local names like `../x` or `/abs` are left as-is)
- `{shared.config.json}` → `/tmp/dats-xxx/shared/config.json` (file-wide, same locality rule as `{outputs.X}`)

Setup commands, teardown commands, and `shared.files` contents expand only `{shared.X}`. The per-test `{inputs.X}`/`{outputs.X}` namespaces stay verbatim there. `inputs.stdin` is never expanded.

`{matrix.X}` is a separate, earlier layer: it is text substitution (not a path), applied once per instance at expansion time — before any runtime expansion, and also. A matrix value may itself contain `{inputs.X}`/`{outputs.X}`/`{shared.X}`, which then expand as usual at runtime. Substituted text is never re-scanned for `{matrix.Y}`. Matrix placeholders are rejected at parse time in setup/teardown commands and `shared.files` contents (`not available outside tests`).

Parent directories of output files declared under `files`/`!files` (e.g. `sub/report.txt`) are created before the command runs. So it can write nested outputs directly. The same goes for nested `shared.files` names.

Commands run with `bash -c` in the working directory of the `dats` invocation. Only fixture files live in the temp directory.

## JSON Schema

`schema.json` provides IDE validation for `.dats` files. Can be used with YAML language servers.

## License

MIT
