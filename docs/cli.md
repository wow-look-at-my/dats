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
