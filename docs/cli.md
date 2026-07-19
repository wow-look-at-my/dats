# CLI Usage

## Synopsis

```
dats [test] [files-or-dirs...] [flags]
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

## Output

Results are printed in a TAP-like format:

```
Running test.dats (3 tests)

ok 1 - echo test
not ok 2 - line matching
  # stdout: line 0: expected to match "^line0$", got "wrong"
ok 3 - exit code test

2/3 passed, 1 failed
```

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
