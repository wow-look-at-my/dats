# Runtime Helpers

The runtime directory (`runtime/`) contains helper functions loaded by all generated BATS tests.

## Directory Structure

```
runtime/
  test_helper.bash              # Main helper file
  test_helper/
    bats-support/               # bats-support library
    bats-assert/                # bats-assert library
```

## Loaded Libraries

The runtime loads these standard BATS libraries:
- **bats-support**: Common helper functions
- **bats-assert**: Assertion functions (`assert_output`, `assert_line`, etc.)

## Exit Code Variables

```bash
EXIT_SUCCESS=0
EXIT_FAILURE=1
```

Use these in `.dats` files:

```yaml
exit: EXIT_SUCCESS
exit: EXIT_FAILURE
```

### Adding Custom Exit Codes

Create your own helper that sources `test_helper.bash`:

```bash
# my_helper.bash
source "$(dirname "$0")/runtime/test_helper.bash"

export EXIT_NOT_FOUND=127
export EXIT_INVALID_INPUT=2
```

## Custom Assertion Functions

### `assert_exit_code`

Assert the exit code of the last `run` command.

```bash
run mycommand
assert_exit_code 0
assert_exit_code $EXIT_SUCCESS
```

**Parameters:**
- `expected`: Expected exit code (integer or variable)

**On failure, prints:**
```
Expected exit code: 0
Actual exit code: 1
Output:
<command output>
```

### `run_with_stderr`

Run a command capturing stdout and stderr separately.

```bash
run_with_stderr mycommand arg1 arg2
```

**Sets these variables:**
- `$status`: Exit code
- `$output`: stdout content
- `$stderr_output`: stderr content
- `$lines`: Array of stdout lines
- `$stderr_lines`: Array of stderr lines

**Use case:** Testing stderr output. Standard `run` merges stdout and stderr.

### `assert_stderr`

Assert stderr contains a pattern. Use after `run_with_stderr`.

```bash
run_with_stderr mycommand
assert_stderr --partial "expected text"
assert_stderr "exact match"
```

**Parameters:**
- `--partial`: Match substring (optional)
- `pattern`: Text to match

### `refute_stderr`

Assert stderr does NOT contain a pattern. Use after `run_with_stderr`.

```bash
run_with_stderr mycommand
refute_stderr --partial "error"
refute_stderr "exact forbidden text"
```

### `setup_dats_tmpdir`

Create a temporary directory for test fixtures.

```bash
setup_dats_tmpdir
echo "data" > "$DATS_TMPDIR/file.txt"
```

**Sets:** `$DATS_TMPDIR` to the temp directory path.

### `teardown_dats_tmpdir`

Clean up the temporary directory.

```bash
teardown_dats_tmpdir
```

**Use in BATS `teardown` function:**

```bash
teardown() {
  teardown_dats_tmpdir
}
```

## Standard bats-assert Functions

These are provided by bats-assert and used by generated tests:

### `assert_output`

```bash
run echo "hello world"
assert_output "hello world"           # exact match
assert_output --partial "hello"       # substring match
assert_output --regexp "^hello"       # regex match
```

### `refute_output`

```bash
run echo "success"
refute_output --partial "error"       # must not contain
```

### `assert_line`

```bash
run printf "a\nb\nc"
assert_line --index 0 "a"             # exact line match
assert_line --index 0 --regexp "^a$"  # regex line match
assert_line --partial "b"             # any line contains
```

### `refute_line`

```bash
run printf "a\nb\nc"
refute_line --partial "error"         # no line contains
```

## Example: Testing Stderr

`.dats` files with stderr checks generate code using `run_with_stderr`:

```yaml
tests:
  - cmd: mycommand
    outputs:
      stderr:
        - "warning"
      "!stderr":
        - "error"
```

Generated:

```bash
@test "mycommand" {
  run_with_stderr mycommand
  assert_exit_code 0
  assert_stderr --partial $'warning'
  refute_stderr --partial $'error'
}
```

## Extending the Runtime

To add project-specific helpers, create a wrapper:

```bash
# test/helper.bash
load "../runtime/test_helper"

# Additional exit codes
export EXIT_TIMEOUT=124
export EXIT_NOT_IMPLEMENTED=38

# Custom assertions
assert_json_valid() {
  jq empty "$1" || {
    echo "Invalid JSON: $1"
    cat "$1"
    return 1
  }
}
```

Then in generated tests, load your helper instead:

```bash
load "helper"  # loads test/helper.bash which loads runtime/test_helper
```
