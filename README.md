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
```

### Subcommands

| Command | Description |
|---------|-------------|
| `test` | Run tests from `.dats` files (default when no subcommand given) |
| `syntax` | Validate `.dats` file syntax without executing tests |

### Flags

| Flag | Scope | Description |
|------|-------|-------------|
| `-v, --verbose` | Global | Show verbose output |
| `--keep-temp` | `test` | Keep temp directory for debugging |
| `--coverdir` | `test` | Set GOCOVERDIR on executed commands to collect coverage data |

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
| `exit` | No | Expected exit code (default: 0). Int or `EXIT_SUCCESS`/`EXIT_FAILURE` |
| `inputs.stdin` | No | Content piped to command's stdin |
| `inputs.files` | No | Map of filename → content (creates fixture files) |
| `outputs.stdout` | No | Patterns to match in stdout |
| `outputs.stderr` | No | Patterns to match in stderr |
| `outputs.!stdout` | No | Patterns that must NOT appear in stdout |
| `outputs.!stderr` | No | Patterns that must NOT appear in stderr |
| `outputs.files` | No | Map of filename → FileCheck for output file validation |

### Output Assertions

- `stdout` / `stderr` - List of patterns to match (substring), or map of line numbers (0-indexed) to regex patterns
- `!stdout` / `!stderr` - Patterns that must NOT appear in output
- `files` - Map of output filename to FileCheck with `exists`, `match`, and `notMatch` properties

### Placeholder System

Commands use `{inputs.X}` and `{outputs.X}` which expand to absolute paths in a temp directory:
- `{inputs.foo.txt}` → `/tmp/dats-xxx/test-N/inputs/foo.txt`
- `{outputs.result.txt}` → `/tmp/dats-xxx/test-N/outputs/result.txt`

## JSON Schema

`schema.json` provides IDE validation for `.dats` files. Can be used with YAML language servers.

## License

MIT
