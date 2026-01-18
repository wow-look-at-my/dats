# DATS - Declarative Automated Testing System

A Go CLI that converts declarative YAML test definitions into [BATS](https://github.com/bats-core/bats-core) (Bash Automated Testing System) test files.

## Installation

```bash
just build
just install  # symlinks to ~/.local/bin/dats
```

## Usage

```bash
dats tests.dats output/
```

This generates `output/tests.gen.bats` which can be run with `bats output/*.gen.bats`.

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
      input.txt: |
        Hello, world!
    cmd: cat {inputs.input.txt}
    outputs:
      stdout:
        - "Hello, world!"

  # Command with stdin
  - desc: cat reads stdin
    stdin: "Hello from stdin"
    cmd: cat
    outputs:
      stdout:
        - "Hello from stdin"

  # Expected non-zero exit
  - desc: grep returns 1 when not found
    exit: 1
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

  # Exit code variables
  - desc: exit code variable
    exit: EXIT_SUCCESS
    cmd: true
```

### Test Properties

| Property | Required | Description |
|----------|----------|-------------|
| `cmd` | Yes | Command to run. Use `{inputs.X}` and `{outputs.X}` for file paths |
| `desc` | No | Optional description for the test |
| `exit` | No | Expected exit code (default: 0). Can be int or `EXIT_*` variable |
| `stdin` | No | Content piped to command's stdin |
| `inputs` | No | Map of filename to content - creates fixture files |
| `outputs` | No | Validation block for stdout, stderr, and output files |

### Output Assertions

- `stdout` / `stderr` - List of patterns to match, or map of line numbers to regex patterns
- `!stdout` / `!stderr` - Patterns that must NOT appear
- Other keys are treated as output file checks with `exists` and `contains` properties

## VS Code Extension

Provides syntax highlighting and schema validation for `.dats` files.

```bash
just vscode::build           # builds to build/dats.vsix
code --install-extension build/dats.vsix
```

## License

MIT
