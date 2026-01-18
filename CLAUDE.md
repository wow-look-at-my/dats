# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

DATS (Declarative Automated Testing System) is a Go CLI tool that converts `.dats` YAML files into BATS (Bash Automated Testing System) test files. It also includes a VS Code extension for syntax highlighting and validation.

## Build Commands

```bash
just build          # Build the dats binary (runs go fmt, go vet, go build)
just test           # Run all tests (Go tests + VS Code tests + BATS examples)
just clean          # Clean build artifacts (git clean -Xdf)
just install        # Symlink binary to ~/.local/bin/dats
```

For VS Code extension:
```bash
just vscode::build   # Build extension and package as .vsix
just vscode::install # Build and install in VS Code
just vscode::update  # Update npm dependencies
```

## Running Specific Tests

```bash
# Run only Go unit tests
go test ./src/dats/...

# Run only VS Code extension tests
cd src/vscode-dats && npm test

# Run specific BATS test file
bats examples/example.gen.bats

# Generate BATS from a .dats file
./dats examples/example.dats examples/ --runtime-dir=runtime
```

## Architecture

### Core Flow
1. `.dats` YAML file is parsed using `gopkg.in/yaml.v3`
2. Generator produces `.gen.bats` file and a `.gen.bats.d` Make dependency file
3. Input files defined in tests are written to `fixtures/<basename>/<test-index>/inputs/`
4. BATS tests use `$BATS_TEST_DIRNAME` for portable path resolution

### Go Package Structure
- `src/dats/main.go` - CLI entry point, argument parsing, runtime directory discovery
- `src/dats/internal/schema/types.go` - YAML schema types (TestFile, Test, OutputBlock, ExitCode)
- `src/dats/internal/generator/generator.go` - BATS generation logic, placeholder expansion

### Key Types
- **ExitCode** - Can be int (0-255) or string like `EXIT_SUCCESS` (references bash variable)
- **OutputCheck** - Either `[]string` (patterns) or `map[int]string` (line-specific regex assertions)
- **OutputBlock** - Handles stdout, stderr, !stdout (negated), !stderr (negated), and arbitrary output file checks

### Placeholder System
Commands use `{inputs.X}` and `{outputs.X}` which expand to `"$BATS_TEST_DIRNAME/fixtures/.../<filename>"`.

### Runtime Files
- `runtime/test_helper.bash` - Loads bats-support/bats-assert, defines `assert_exit_code`, `run_with_stderr`, `assert_stderr`, `refute_stderr`
- `runtime/test_helper/bats-support/` and `bats-assert/` - Standard BATS assertion libraries

### VS Code Extension
Located in `src/vscode-dats/`. Provides:
- `.dats` file language registration
- TextMate grammar for syntax highlighting
- JSON Schema validation via `yamlValidation` contribution
- Snippets for test templates

## DATS File Format

```yaml
tests:
  - desc: optional description
    cmd: command to run    # Required, supports {inputs.X} and {outputs.X}
    exit: 0                # Optional, default 0 (or EXIT_* variable)
    stdin: "input"         # Optional, piped to cmd
    inputs:                # Optional, creates fixture files
      file.txt: content
    outputs:               # Optional
      stdout:              # Pattern list or line-number map
        - "pattern"
        0: "^first line$"  # Line-specific regex
      "!stdout":           # Patterns that must NOT appear
        - "error"
```
