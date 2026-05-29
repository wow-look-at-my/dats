# CLI Usage

## Synopsis

```
dats [test] [files...] [flags]
dats syntax [files...] [flags]
```

`dats` runs tests directly; there is no separate build/generate step. If no files are given,
it recursively discovers all `.dats` files in the current directory tree.

## Commands

| Command | Description |
|---------|-------------|
| `test` | Run tests from the given `.dats` files. This is the default action, so `dats file.dats` and `dats test file.dats` are equivalent. |
| `syntax` | Parse and validate `.dats` files without executing any tests. |

## Flags

| Flag | Description |
|------|-------------|
| `-v, --verbose` | Show command details, durations, and full output on failure |
| `--keep-temp` | Keep the per-run temp directory (prints its path) for debugging |
| `--coverdir <dir>` | Set `GOCOVERDIR` on executed commands to collect coverage data |

## Examples

```bash
# Run a specific file (positional, or via the test subcommand)
dats test.dats
dats test test.dats

# Run every .dats file under the current directory
dats test

# Verbose output
dats -v test.dats

# Keep the temp directory to inspect fixtures/outputs
dats --keep-temp test.dats

# Validate syntax without running
dats syntax test.dats
dats syntax            # validate all .dats files in the tree

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
