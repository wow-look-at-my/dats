# DATS - Declarative Automated Testing System

A Go CLI that runs tests defined in declarative YAML files (`.dats`). It natively executes commands, captures output, and verifies assertions without requiring external test frameworks.

## Installation

```bash
just build          # Build the dats binary to build/dats
just install        # Symlink binary to ~/.local/bin/dats
```

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

# Keep temp directory for debugging
dats test --keep-temp tests.dats

# Validate .dats file syntax without running tests
dats syntax tests.dats

# Validate all .dats files in current directory tree
dats syntax

# Print the version
dats version
```

Both `test` and `syntax` accept any mix of `.dats` files and directories.
Directory arguments and no-arg discovery recurse the tree, skipping hidden
directories and hidden `.dats` files (leading `.`); explicitly named files are
always accepted. Repeated arguments are deduplicated by absolute path.

### Subcommands

| Command | Description |
|---------|-------------|
| `test` | Run tests from `.dats` files or directories (default when no subcommand given) |
| `syntax` | Validate `.dats` file syntax without executing tests |
| `version` | Print a one-line `dats <version>` |

### Flags

| Flag | Scope | Description |
|------|-------|-------------|
| `-v, --verbose` | Global | Show verbose output |
| `--keep-temp` | Global | Keep temp directory for debugging |
| `--coverdir` | Global | Set GOCOVERDIR on executed commands to collect coverage data |
| `--version` | Root | Print `dats <version>` (same output as `dats version`) |

## DATS File Format

```yaml
tests:
  # Simple command
  - desc: echo test
    cmd: echo Hello World
    outputs:
      stdout:
        - "Hello World"

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
      "!stdout":
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
      "!files":
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
```

### Test Properties

| Property | Required | Description |
|----------|----------|-------------|
| `cmd` | Yes | Command to run. Use `{inputs.X}` and `{outputs.X}` for file paths |
| `desc` | No | Description for the test (used in output) |
| `exit` | No | Expected exit code (default: 0). Int 0-255 (bare or quoted, e.g. `"3"`) or `EXIT_SUCCESS`/`EXIT_FAILURE`; floats are rejected at parse time |
| `timeout` | No | Per-test timeout: integer seconds (bare or quoted, e.g. `"5"`) or a Go duration string (e.g. `500ms`, `2s`, `1m30s`). 0/omitted = no timeout; floats are rejected (write `1.5s`, not `1.5`) |
| `inputs.stdin` | No | Content piped to command's stdin |
| `inputs.files` | No | Map of filename → content (creates fixture files) |
| `inputs.env` | No | Map of env var name → value, added to the inherited environment (values go through placeholder expansion) |
| `outputs.stdout` | No | Patterns to match in stdout |
| `outputs.stderr` | No | Patterns to match in stderr |
| `outputs.!stdout` | No | Patterns that must NOT appear in stdout |
| `outputs.!stderr` | No | Patterns that must NOT appear in stderr |
| `outputs.files` | No | Map of filename → FileCheck for output file validation; an empty check (`{}` or null) means "must exist" |
| `outputs.!files` | No | Map of filename → FileCheck with each check inverted (e.g. `exists: true` = must NOT exist; empty check = must NOT exist) |
| `outputs.json_output` | No | Expected JSON value of the whole stdout (deep equality) |

File names under `inputs.files`, `outputs.files`, and `outputs.!files` must be
relative paths that stay inside the test directory (no `..` or absolute paths;
rejected at parse time, so `dats syntax` catches it). Nested names like
`sub/file.txt` are allowed.

### Output Assertions

- `stdout` / `stderr` - List of patterns to match (substring), or map of line numbers (0-indexed) to regex patterns
- `!stdout` / `!stderr` - Patterns that must NOT appear in output (list of substrings, or map of 0-indexed line numbers to regexes that must not match within that line)
- `files` - Map of output filename to FileCheck with `exists` (bool), `match` (regex patterns that must match), and `notMatch` (regex patterns that must not match); an empty check is an implicit "must exist"
- `!files` - Same FileCheck shape with each check inverted: `exists` flipped, `match` patterns must NOT match, `notMatch` patterns must match; an empty check is an implicit "must NOT exist"
- `json_output` - Expected JSON value of the whole stdout: keys order-insensitive, arrays order-sensitive, numbers by value

### Failure Reporting

- A command that exceeds its `timeout` has its whole process group killed
  (background children included) and fails with only `command timed out after
  X` — all other assertions are skipped, since checking partial output would
  bury the real cause. Captured stdout/stderr are still shown in verbose mode.
- A command killed by a signal names it in the exit-code failure, e.g.
  `expected exit code 0, got -1 (killed by signal: killed)`.
- Multiple failing file checks report in sorted-by-name order.
- Commands that leave orphaned background processes holding stdout/stderr do
  not block the runner: the pipes are force-closed about 1 second after the
  main process exits, and anything stragglers write after that is not captured.

### Placeholder System

Commands, `inputs.files` contents, and `inputs.env` values use `{inputs.X}` and `{outputs.X}`, which expand to absolute paths in a temp directory:
- `{inputs.foo.txt}` → `/tmp/dats-xxx/test-N/inputs/foo.txt` (X must be declared under `inputs.files`; otherwise left as-is)
- `{outputs.result.txt}` → `/tmp/dats-xxx/test-N/outputs/result.txt` (no `files` check required, as long as X is a local relative path; non-local names like `../x` or `/abs` are left as-is)

Parent directories of output files declared under `files`/`!files` (e.g. `sub/report.txt`) are created before the command runs, so it can write nested outputs directly.

Commands run with `bash -c` in the working directory of the `dats` invocation; only fixture files live in the temp directory.

## JSON Schema

`schema.json` provides IDE validation for `.dats` files. Can be used with YAML language servers.

## License

MIT
