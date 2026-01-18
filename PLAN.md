# Plan: Replace BATS with Native Test Runner

## Summary

Drop the intermediate BATS generation step. Instead of `dats generate foo.dats` producing `.gen.bats` files that require BATS to run, the `dats` binary will directly execute tests from `.dats` files.

**Current flow:**
```
.dats → (generator) → .gen.bats → (bats) → results
```

**New flow:**
```
.dats → (dats run) → results
```

## Why Drop BATS

1. **Extra dependency** - Users need BATS, bats-support, bats-assert installed
2. **Two-step process** - Generate then run is unnecessary indirection
3. **Limited value** - We only use a small subset of BATS features:
   - `run` to capture exit code and output
   - `assert_output --partial` / `refute_output --partial`
   - `assert_line --index N --regexp`
   - Exit code comparison
4. **Simpler debugging** - Native runner can provide better error messages

## CLI Interface (unchanged)

```bash
# Run tests directly (same invocation as before)
dats examples/example.dats

# Verbose output
dats -v examples/example.dats
```

The only change is behavior: instead of generating a `.gen.bats` file, it runs the tests and prints results.

## Implementation Plan

### Phase 1: Core Runner Infrastructure

**File: `internal/runner/runner.go`**

```go
type Runner struct {
    Verbose bool
    WorkDir string  // temp directory for fixtures
}

type TestResult struct {
    Test     *schema.Test
    Passed   bool
    Duration time.Duration
    Failures []string  // list of assertion failures
}

func (r *Runner) RunFile(path string) ([]TestResult, error)
func (r *Runner) RunTest(test *schema.Test) TestResult
```

### Phase 2: Command Execution

**File: `internal/runner/exec.go`**

Execute commands and capture:
- Exit code
- Stdout (as string and split into lines)
- Stderr (as string and split into lines)

```go
type ExecResult struct {
    ExitCode int
    Stdout   string
    StdoutLines []string
    Stderr   string
    StderrLines []string
}

func Execute(cmd string, stdin string, env []string) (*ExecResult, error)
```

Use `os/exec` with:
- `cmd.Stdin` for stdin piping
- `cmd.Stdout` / `cmd.Stderr` as `bytes.Buffer`
- Check `cmd.ProcessState.ExitCode()` for exit code

### Phase 3: Assertion Engine

**File: `internal/runner/assert.go`**

```go
// Check if pattern appears in text (substring match)
func AssertContains(text, pattern string) error
func RefuteContains(text, pattern string) error

// Check if line N matches regex
func AssertLineRegex(lines []string, lineNum int, pattern string) error

// Check exit code
func AssertExitCode(actual int, expected schema.ExitCode) error

// File assertions
func AssertFileExists(path string) error
func RefuteFileExists(path string) error
func AssertFileMatches(path string, patterns []string) error
func RefuteFileMatches(path string, patterns []string) error
```

### Phase 4: Fixture Management

**File: `internal/runner/fixtures.go`**

- Create temp directory per test file
- Write input files to `<tmpdir>/<test-index>/inputs/`
- Resolve `{inputs.X}` and `{outputs.X}` placeholders
- Cleanup on completion (or preserve with flag for debugging)

### Phase 5: Output Formatting

**File: `internal/runner/output.go`**

TAP-like output:
```
Running examples/example.dats (5 tests)

ok 1 - echo test
ok 2 - cat reads file
not ok 3 - line matching
  # Expected line 0 to match "^line0$", got "wrongline"
ok 4 - exit code test
ok 5 - negated pattern test

4/5 passed, 1 failed
```

Verbose mode shows:
- Command being run
- Full stdout/stderr on failure
- Assertion details

### Phase 6: Update CLI

**File: `main.go`**

Same interface, different behavior:
- Parse `.dats` file
- Call runner instead of generator
- Print results, exit non-zero on failure

## Files to Modify

| File | Action |
|------|--------|
| `main.go` | Add `run` subcommand, wire up runner |
| `internal/runner/runner.go` | **NEW** - Core test runner |
| `internal/runner/exec.go` | **NEW** - Command execution |
| `internal/runner/assert.go` | **NEW** - Assertion functions |
| `internal/runner/fixtures.go` | **NEW** - Fixture file management |
| `internal/runner/output.go` | **NEW** - Result formatting |

## Files to Remove

| File | Reason |
|------|--------|
| `internal/generator/generator.go` | No longer generating BATS |
| `internal/generator/generator_test.go` | Tests for removed generator |
| `runtime/test_helper.bash` | BATS helper no longer needed |
| `runtime/test_helper/bats-support/` | BATS library |
| `runtime/test_helper/bats-assert/` | BATS library |
| `examples/*.gen.bats` | Generated files no longer produced |

## Verification

1. Run `dats run examples/example.dats` - should execute all tests
2. Verify exit code 0 when all tests pass, non-zero when any fail
3. Verify verbose mode shows useful debugging info
4. Verify fixture cleanup happens (or doesn't with debug flag)
5. Compare behavior against current BATS-based execution for same .dats files

## Migration Notes

- Existing `.gen.bats` files will no longer be produced
- Users who run BATS directly will need to switch to `dats run`
- The `.dats` file format remains unchanged
- justfile `test` recipe will change from `bats examples/*.gen.bats` to `dats run examples/*.dats`
