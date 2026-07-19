# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

DATS (Declarative Automated Testing System) is a Go CLI that runs tests defined in declarative XML files (`.dats`). It natively executes commands, captures output, and verifies assertions without requiring external test frameworks.

## Build Commands

```bash
just generate       # Regenerate Go types from schema/dats.xsd (requires xgen)
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

# Parallel execution (4 workers; bare -j = one per CPU; -j 4 does NOT bind)
./build/dats -j4 examples/example.dats

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
- `schema/dats.xsd` - Canonical XSD schema (source of truth for types)
- `internal/schema/generated.go` - Go types generated from XSD via `xgen` (DO NOT EDIT)
- `internal/schema/types.go` - Custom methods on generated types (ExitCode validation, accessors)
- `internal/runner/` - Native test runner
  - `runner.go` - Orchestrates test execution (RunFile, RunTest)
  - `exec.go` - Command execution via bash, captures exit code and output
  - `fixtures.go` - Creates input files, expands `{inputs.X}` and `{outputs.X}` placeholders
  - `assert.go` - Assertion functions (AssertContains, AssertLineRegex, AssertExitCode, etc.)
  - `output.go` - Result types (TestResult, FileResult with SetupFailure/TeardownFailures + Ok(), CommandFailure) and TAP-like formatting (PrintHookFailure diagnostics, `teardown failed` summary annotation)
- `report/` - Machine-readable report rendering (public, importable by external modules)
  - `junit.go` - `WriteJUnit`: JUnit XML (testsuites/testsuite/testcase; failed instances carry failure + system-out/err; synthetic `[setup]` first / `[teardown]` trailing cases for hook failures, counted in the tests/failures attrs so JUnit totals ≥ CLI counts) + the XML 1.0 control-char sanitizer (illegal runes → U+FFFD)
  - `json.go` - `WriteJSON`: JSON report (`format_version` 1; summary counts = CLI instance counts; hook failures in setup_failure/teardown_failures; stdout/stderr keys present exactly on failed instances). Field names are a stability contract — see `docs/reports.md` before changing anything here
- `docs/` - Additional prose documentation (`reports.md` = report formats + stability contract); `schema.json` - JSON Schema for IDE validation

### Key Types (generated from XSD)
- **Dats** - Root `<dats>` element containing `[]*Test`
- **Test** - Attributes: `DescAttr`, `CmdAttr`, `ExitAttr`. Children: `Stdin`, `Input`, `Stdout`, `Stderr`, `Output`
- **ExitCode** - `string` type with custom `UnmarshalXMLAttr` + accessor methods (`IntValue()`, `IsVariable()`, `VariableName()`)
- **StreamCheck** - `<stdout>`/`<stderr>` with `Match`, `NotMatch`, and `Line` children
- **InputFile** - `<input name="file.txt">content</input>` — fields: `NameAttr`, `Value`
- **FileOutput** - `<output name="file.txt" exists="true">` — fields: `NameAttr`, `ExistsAttr *bool`, `Match`, `NotMatch`
- **LineCheck** - `<line n="0">pattern</line>` — fields: `NAttr`, `Value`

### XML Design: Attributes vs Children
XML provides a natural distinction between properties ON an object (attributes) and properties IN an object (children):
- **Attributes** = scalar metadata about the test: `desc`, `cmd`, `exit`
- **Children** = structured content within the test: `<stdin>`, `<input>`, `<stdout>`, `<output>`

### Placeholder System
Commands, `inputs.files` contents, and `inputs.env` values use `{inputs.X}`, `{outputs.X}`, and `{shared.X}`, which expand to absolute paths in the temp directory:
- `{inputs.foo.txt}` → `/tmp/dats-xxx/test-N/inputs/foo.txt` (X must be declared under `inputs.files`; otherwise left as-is)
- `{outputs.result.txt}` → `/tmp/dats-xxx/test-N/outputs/result.txt` (no `outputs.files` check required, as long as X is a local relative path; non-local names are left as-is)
- `{shared.config.json}` → `/tmp/dats-xxx/shared/config.json` (file-wide directory shared by all tests; no declaration required, same locality rule as `{outputs.X}`)

Setup commands, teardown commands, and `shared.files` contents expand ONLY `{shared.X}`; `{inputs.X}`/`{outputs.X}` stay verbatim there. `inputs.stdin` is never expanded.

`{matrix.X}` is a separate, earlier layer: single-pass text substitution at instance-expansion time (before any runtime expansion), also reaching `desc`, `inputs.stdin`, output patterns, and json_output strings. Matrix values may contain other placeholders (expanded at runtime as usual); substituted text is never re-scanned. Matrix placeholders in setup/teardown/shared are parse errors (`not available outside tests`); fixture file NAMES and env var NAMES are never substituted.

Fixture names (`inputs.files`, `outputs.files`, `outputs.!files`, `shared.files`) must be local relative paths (no `..`/absolute; enforced at parse time and again at fixture setup). Nested names like `sub/file.txt` are allowed; parent directories of declared output files and of shared files are auto-created.

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
