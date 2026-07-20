# Runtime Helpers

DATS includes a set of helper functions that are automatically available in all generated tests. These helpers are embedded in the `dats` binary and extracted at test time.

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

## DATS Helper Functions

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
