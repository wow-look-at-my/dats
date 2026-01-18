# Plan: Fix DATS Documentation + Code Issues

## Issues Identified

1. **Examples are fabricated** - Need to generate real examples by running dats
2. **Docs explain bypassing dats** - Remove references to manually editing generated files, calling bats directly, etc.
3. **Runtime helpers doc is confusing** - Mixing DATS-specific helpers with bats-assert documentation
4. **--runtime-dir shouldn't be documented** - It auto-discovers; remove from docs
5. **bats-assert/bats-support not embedded** - Should be embedded in binary (MIT license)
6. **dats should run tests** - Currently only generates; needs to execute bats after generation

## Plan

### Part A: Code Changes

#### A1. Embed runtime files in the dats binary
The bats-support and bats-assert libraries (MIT license) should be embedded directly in the Go executable using `go:embed`. This eliminates runtime directory discovery entirely.

**New package: `internal/runtime/`**
- Create `internal/runtime/embedded.go`:
  ```go
  //go:embed files/*
  var Files embed.FS
  ```
- Move `runtime/` contents to `internal/runtime/files/`
- Remove orphaned `.git` files from bats-assert/bats-support

**Changes to generator:**
- Generated .bats files no longer use relative `load` paths
- Instead, dats writes helper files to temp dir at test time

#### A2. Add test execution to dats
After generating the .bats file, dats should run bats automatically:

1. Create temp directory
2. Extract embedded runtime files to temp dir
3. Run `bats <generated.bats>` with runtime in temp
4. Stream bats output to stdout/stderr
5. Pass through bats exit code
6. Clean up temp dir

#### A3. Remove --runtime-dir and findRuntimeDir()
- Delete all runtime directory discovery code from main.go
- Delete `--runtime-dir` argument parsing
- Delete `findRuntimeDir()` function
- CLI becomes simply: `dats <file.dats> [output_dir]`

#### A4. Update generator to use embedded runtime
- Modify `internal/generator/generator.go`
- Generated .bats files should load helper from a path provided at generation time
- Or: Generate self-contained tests that don't need external helpers

### Part B: Documentation Changes

#### B1. Rewrite docs/README.md
- Remove "Run the test: `bats`" step
- Show: `dats mytest.dats` generates AND runs tests

#### B2. Rewrite docs/cli.md
- Remove `--runtime-dir` from synopsis and examples
- Remove "Integration with BATS" section
- Remove Make integration examples
- Keep simple: `dats <file.dats> [output_dir]`

#### B3. Rewrite docs/runtime.md
- Focus ONLY on DATS-specific helpers:
  - `assert_exit_code`
  - `run_with_stderr` / `assert_stderr` / `refute_stderr`
  - `setup_dats_tmpdir` / `teardown_dats_tmpdir`
  - `EXIT_SUCCESS` / `EXIT_FAILURE`
- Remove bats-assert function docs (external library)
- Remove "extending the runtime" section

#### B4. Rewrite docs/examples.md
- Reference real `examples/example.dats` file content
- Don't invent fake examples

#### B5. Rewrite docs/generated-output.md
- Use real generated output from `examples/example.gen.bats`
- Remove any fabricated transformation examples

## Files to Modify

### Code
- `main.go` - Add bats execution, remove --runtime-dir, remove findRuntimeDir()
- `internal/generator/generator.go` - Update load path handling
- `internal/runtime/` (NEW) - Create package with embedded files
- `runtime/` - Move to `internal/runtime/files/`, delete orphaned .git files

### Docs
- `docs/README.md` - Update quick start
- `docs/cli.md` - Remove --runtime-dir, remove bats invocation
- `docs/runtime.md` - Remove bats-assert docs, focus on DATS helpers
- `docs/examples.md` - Replace with real example.dats content
- `docs/generated-output.md` - Use real example.gen.bats content
- `docs/file-format.md` - Keep as-is (syntax reference is fine)

## Verification
1. `just build` - Ensure dats compiles with embedded runtime
2. `dats examples/example.dats examples/` - Should generate AND run tests automatically
3. Verify no external runtime files needed - binary is self-contained
4. Review all docs contain only real examples and no bypass instructions
