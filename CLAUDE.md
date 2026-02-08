# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

DATS (Declarative Automated Testing System) is a Go CLI that runs tests defined in declarative XML files (`.dats`). It natively executes commands, captures output, and verifies assertions without requiring external test frameworks.

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
1. `.dats` XML file is parsed using `encoding/xml` (stdlib)
2. For each test, fixtures are set up in a temp directory
3. Command is executed via `bash -c` with placeholder expansion
4. Exit code, stdout, stderr, and output files are validated against assertions
5. Results are printed in TAP-like format

### Go Package Structure
- `main.go` - CLI entry point, argument parsing, file validation
- `internal/schema/types.go` - XML schema types with custom attribute unmarshalers
- `internal/runner/` - Native test runner
  - `runner.go` - Orchestrates test execution (RunFile, RunTest)
  - `exec.go` - Command execution via bash, captures exit code and output
  - `fixtures.go` - Creates input files, expands `{inputs.X}` and `{outputs.X}` placeholders
  - `assert.go` - Assertion functions (AssertContains, AssertLineRegex, AssertExitCode, etc.)
  - `output.go` - Result types (TestResult, FileResult) and TAP-like formatting

### Key Types
- **TestFile** - Root `<dats>` element containing `[]Test`
- **Test** - Attributes: `desc`, `cmd`, `exit`. Children: `stdin`, `input`, `stdout`, `stderr`, `output`
- **ExitCode** - Custom XML attr unmarshaler: int (0-255) or `EXIT_SUCCESS`/`EXIT_FAILURE`
- **StreamCheck** - `<stdout>`/`<stderr>` with `<match>`, `<not-match>`, and `<line n="N">` children
- **InputFile** - `<input name="file.txt">content</input>`
- **FileOutput** - `<output name="file.txt" exists="true">` with `<match>`/`<not-match>` children
- **ExistsBool** - Custom XML attr unmarshaler tracking whether `exists` was explicitly set

### XML Design: Attributes vs Children
XML provides a natural distinction between properties ON an object (attributes) and properties IN an object (children):
- **Attributes** = scalar metadata about the test: `desc`, `cmd`, `exit`
- **Children** = structured content within the test: `<stdin>`, `<input>`, `<stdout>`, `<output>`

### Placeholder System
Commands use `{inputs.X}` and `{outputs.X}` which expand to absolute paths in the temp directory:
- `{inputs.foo.txt}` → `/tmp/dats-xxx/test-N/inputs/foo.txt`
- `{outputs.result.txt}` → `/tmp/dats-xxx/test-N/outputs/result.txt`

## DATS File Format

```xml
<dats>
  <test desc="optional description" cmd="command to run" exit="0">
    <!-- Input: stdin content piped to cmd -->
    <stdin>input text</stdin>

    <!-- Input: fixture files created before running cmd -->
    <input name="file.txt">content</input>

    <!-- Output: stdout assertions -->
    <stdout>
      <match>pattern</match>           <!-- Substring match -->
      <not-match>error</not-match>     <!-- Must NOT appear -->
      <line n="0">^first line$</line>  <!-- Line-specific regex (0-indexed) -->
    </stdout>

    <!-- Output: stderr assertions -->
    <stderr>
      <match>warning</match>
    </stderr>

    <!-- Output: file assertions -->
    <output name="result.txt" exists="true">
      <match>expected content</match>
      <not-match>error</not-match>
    </output>
  </test>
</dats>
```

### Test Attributes

| Attribute | Required | Description |
|-----------|----------|-------------|
| `cmd` | Yes | Command to run. Use `{inputs.X}` and `{outputs.X}` for file paths |
| `desc` | No | Description for the test (used in output) |
| `exit` | No | Expected exit code (default: 0). Int or `EXIT_SUCCESS`/`EXIT_FAILURE` |

### Test Children

| Element | Description |
|---------|-------------|
| `<stdin>` | Content piped to command's stdin |
| `<input name="X">` | Fixture file created before running cmd |
| `<stdout>` | Stdout assertions (`<match>`, `<not-match>`, `<line>`) |
| `<stderr>` | Stderr assertions (`<match>`, `<not-match>`, `<line>`) |
| `<output name="X">` | Output file validation with optional `exists` attr |

## CI/CD

GitHub Actions workflow (`.github/workflows/ci.yml`) runs on push:
- Builds Go binary for multiple platforms
- Runs tests
- Creates releases on master branch pushes
