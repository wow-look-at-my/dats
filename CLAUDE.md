# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

DATS (Declarative Automated Testing System) is a Go CLI tool that converts `.dats` YAML files into BATS (Bash Automated Testing System) test files and runs them. The runtime (bats-support, bats-assert, and custom helpers) is embedded in the binary.

## Build Commands

```bash
go build -o dats .    # Build the dats binary
go test ./...         # Run all Go tests
go fmt ./...          # Format Go code
go vet ./...          # Run Go vet
```

## Running Tests

```bash
# Run Go unit tests
go test ./...

# Run dats on an example (generates and runs tests)
./dats examples/example.dats examples/
```

## Architecture

### Core Flow
1. `.dats` YAML file is parsed using `gopkg.in/yaml.v3`
2. Generator produces `.gen.bats` file and a `.gen.bats.d` Make dependency file
3. Input files defined in tests are written to `fixtures/<basename>/<test-index>/inputs/`
4. Embedded runtime is extracted to a temp directory
5. BATS is run with `DATS_RUNTIME_DIR` set to the temp directory
6. Exit code from BATS is passed through

### Go Package Structure
- `main.go` - CLI entry point, argument parsing, test execution
- `internal/schema/types.go` - YAML schema types (TestFile, Test, OutputBlock, ExitCode)
- `internal/generator/generator.go` - BATS generation logic, placeholder expansion
- `internal/runtime/embedded.go` - Embedded runtime files (bats-support, bats-assert, helpers)
- `internal/runtime/files/` - Source files for embedded runtime

### Key Types
- **ExitCode** - Can be int (0-255) or string like `EXIT_SUCCESS` (references bash variable)
- **OutputCheck** - Either `[]string` (patterns) or `map[int]string` (line-specific regex assertions)
- **OutputBlock** - Handles stdout, stderr, !stdout (negated), !stderr (negated), and arbitrary output file checks

### Placeholder System
Commands use `{inputs.X}` and `{outputs.X}` which expand to `"$BATS_TEST_DIRNAME/fixtures/.../<filename>"`.

### Embedded Runtime
The runtime is embedded in the binary using Go's `embed` package:
- `internal/runtime/files/test_helper.bash` - Main helper, loads bats libraries, defines custom assertions
- `internal/runtime/files/test_helper/bats-support/` - Standard BATS support library
- `internal/runtime/files/test_helper/bats-assert/` - Standard BATS assertion library

## DATS File Format

```yaml
tests:
  - desc: optional description
    cmd: command to run    # Required, supports {inputs.X} and {outputs.X}
    exit: 0                # Optional, default 0 (or EXIT_* variable)
    inputs:                # Optional
      stdin: "input"       # Piped to cmd
      files:               # Creates fixture files
        file.txt: content
    outputs:               # Optional
      stdout:              # Pattern list or line-number map
        - "pattern"
        0: "^first line$"  # Line-specific regex
      "!stdout":           # Patterns that must NOT appear
        - "error"
```
