# DATS File Format Reference

## Root Structure

A `.dats` file contains a single `tests` array:

```yaml
tests:
  - # test 1
  - # test 2
```

## Test Object

Each test has these fields:

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `cmd` | string | Yes | - | Command to execute |
| `desc` | string | No | Value of `cmd` | Test description/name |
| `exit` | int or string | No | `0` | Expected exit code |
| `inputs` | object | No | - | Stdin and input files |
| `outputs` | object | No | - | Output validations |

### Minimal Test

```yaml
tests:
  - cmd: echo hello
```

### Full Test

```yaml
tests:
  - desc: comprehensive example
    exit: 0
    cmd: process {inputs.data.txt} -o {outputs.result.txt}
    inputs:
      stdin: "optional stdin content"
      files:
        data.txt: "input file content"
    outputs:
      stdout:
        - "pattern to match"
      stderr:
        - "expected stderr"
      "!stdout":
        - "must not appear"
      files:
        result.txt:
          exists: true
          match:
            - "expected content"
```

---

## Command Field (`cmd`)

The command supports placeholders for input and output files:

### Input Placeholders

`{inputs.<filename>}` expands to the path of an input fixture file.

```yaml
inputs:
  files:
    data.txt: "content"
cmd: cat {inputs.data.txt}
```

Generates: `cat "$BATS_TEST_DIRNAME/fixtures/<basename>/<index>/inputs/data.txt"`

### Output Placeholders

`{outputs.<filename>}` expands to a path where the command should write output.

```yaml
cmd: process -o {outputs.result.bin}
outputs:
  files:
    result.bin:
      exists: true
```

Generates: `process -o "$BATS_TEST_DIRNAME/fixtures/<basename>/<index>/outputs/result.bin"`

### Multiple Placeholders

```yaml
cmd: diff {inputs.a.txt} {inputs.b.txt} > {outputs.diff.txt}
```

---

## Exit Code Field (`exit`)

### Integer Values (0-255)

```yaml
exit: 0      # success
exit: 1      # generic failure
exit: 127    # command not found
```

### Variable Names

Must match pattern `^EXIT_[A-Z_]+$`:

```yaml
exit: EXIT_SUCCESS   # expands to $EXIT_SUCCESS
exit: EXIT_FAILURE   # expands to $EXIT_FAILURE
```

Built-in variables defined in `runtime/test_helper.bash`:
- `EXIT_SUCCESS` = 0
- `EXIT_FAILURE` = 1

You can define additional variables in your own helper file.

---

## Inputs Block

```yaml
inputs:
  stdin: "content piped to stdin"
  files:
    filename.txt: "file content"
    another.txt: |
      multi-line
      content
```

### `stdin`

Content piped to the command via bash here-string (`<<<`):

```yaml
inputs:
  stdin: "hello world"
cmd: grep hello
```

Generates: `run bash -c "grep hello" <<< $'hello world'`

### `files`

Map of filename to content. Creates fixture files before test runs:

```yaml
inputs:
  files:
    config.json: |
      {"key": "value"}
    data.csv: "a,b,c"
```

Reference these files in the command with `{inputs.<filename>}`.

---

## Outputs Block

```yaml
outputs:
  stdout:        # patterns that MUST appear
  stderr:        # patterns that MUST appear
  "!stdout":     # patterns that must NOT appear
  "!stderr":     # patterns that must NOT appear
  files:         # output file checks
```

### Pattern Lists

Match patterns anywhere in output:

```yaml
outputs:
  stdout:
    - "expected text"
    - "another pattern"
```

Generates:
```bash
assert_output --partial $'expected text'
assert_output --partial $'another pattern'
```

### Line-Specific Checks

Use integer keys (0-indexed) with regex patterns:

```yaml
outputs:
  stdout:
    0: "^first line$"
    2: "^third line$"
    5: "pattern on line 6"
```

Generates:
```bash
assert_line --index 0 --regexp $'^first line$'
assert_line --index 2 --regexp $'^third line$'
assert_line --index 5 --regexp $'pattern on line 6'
```

**Note**: You cannot mix pattern lists and line-specific checks in the same block. Use one format or the other.

### Negated Output Checks

`!stdout` and `!stderr` assert patterns do NOT appear:

```yaml
outputs:
  "!stdout":
    - "error"
    - "failed"
  "!stderr":
    - "warning"
```

Generates:
```bash
refute_output --partial $'error'
refute_output --partial $'failed'
```

---

## File Checks

Validate output files created by the command:

```yaml
outputs:
  files:
    output.bin:
      exists: true
      match:
        - "expected pattern"
        - "another pattern"
      notMatch:
        - "should not contain"
```

### Fields

| Field | Type | Description |
|-------|------|-------------|
| `exists` | boolean | Whether the file should exist |
| `match` | string[] | Regex patterns that must appear (uses `grep -qE`) |
| `notMatch` | string[] | Regex patterns that must NOT appear |

### Negative File Checks

Use `!files` to assert files do NOT exist:

```yaml
outputs:
  "!files":
    error.log:
      exists: false
```

---

## Complete Field Reference

```yaml
tests:
  - desc: string           # optional, defaults to cmd value
    exit: int|string       # optional, defaults to 0
    cmd: string            # required
    inputs:
      stdin: string        # optional
      files:               # optional
        <name>: string     # filename: content
    outputs:
      stdout: []|{}        # pattern list or line checks
      stderr: []|{}        # pattern list or line checks
      "!stdout": []|{}     # negated patterns
      "!stderr": []|{}     # negated patterns
      files:
        <name>:
          exists: bool
          match: []
          notMatch: []
      "!files":
        <name>:
          exists: bool
```
