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

# Keep temp directory for debugging
./build/dats --keep-temp examples/example.dats
```

## Architecture

### Core Flow
1. `.dats` YAML file is parsed using `gopkg.in/yaml.v3`
2. For each test, fixtures are set up in a temp directory
3. Command is executed via `bash -c` with placeholder expansion
4. Exit code, stdout, stderr, and output files are validated against assertions
5. Results are printed in TAP-like format

### Go Package Structure
- `main.go` - CLI entry point, argument parsing, file validation
- `schema/types.go` - YAML schema types with custom unmarshalers (public, importable by external modules)
- `runner/` - Native test runner (public, importable by external modules)
  - `runner.go` - Orchestrates test execution (RunFile, RunTest)
  - `exec.go` - Command execution via bash, captures exit code and output
  - `fixtures.go` - Creates input files, expands `{inputs.X}` and `{outputs.X}` placeholders
  - `assert.go` - Assertion functions (AssertContains, AssertLineRegex, AssertExitCode, etc.)
  - `output.go` - Result types (TestResult, FileResult) and TAP-like formatting

### Key Types
- **ExitCode** - Can be int (0-255) or string like `EXIT_SUCCESS`/`EXIT_FAILURE`
- **OutputCheck** - Either `[]string` (patterns) or `map[int]string` (line-specific regex, 0-indexed)
- **OutputBlock** - Handles stdout, stderr, !stdout, !stderr, and file checks
- **FileCheck** - Validates output files with `exists`, `match`, and `notMatch` properties
- **InputBlock** - Contains `stdin` (string) and `files` (map of filename to content)

### Placeholder System
Commands use `{inputs.X}` and `{outputs.X}` which expand to absolute paths in the temp directory:
- `{inputs.foo.txt}` → `/tmp/dats-xxx/test-N/inputs/foo.txt`
- `{outputs.result.txt}` → `/tmp/dats-xxx/test-N/outputs/result.txt`

## DATS File Format

```yaml
tests:
  - desc: optional description
    cmd: command to run       # Required, supports {inputs.X} and {outputs.X}
    exit: 0                   # Optional, default 0 (or EXIT_SUCCESS/EXIT_FAILURE)
    inputs:
      stdin: "input text"     # Optional, piped to cmd
      files:                  # Optional, creates fixture files
        file.txt: content
    outputs:                  # Optional
      stdout:                 # Pattern list or line-number map
        - "pattern"           # Substring match
      stdout:                 # Or use line-specific regex (0-indexed)
        0: "^first line$"
        2: "^third line$"
      "!stdout":              # Patterns that must NOT appear
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

## CI/CD

GitHub Actions workflow (`.github/workflows/ci.yml`) runs on push:
- Builds Go binary for multiple platforms
- Runs tests
- Creates releases on master branch pushes

## JSON Schema

`schema.json` provides IDE validation for `.dats` files. Can be used with YAML language servers.
