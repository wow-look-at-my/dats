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
| `timeout` | int or string | No | none | Per-test timeout (seconds or duration string) |
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
    timeout: 5s
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

The command is run with `bash -c` in a fresh per-run temp directory. It supports placeholders
for input and output files:

### Input Placeholders

`{inputs.<filename>}` expands to the absolute path of an input fixture file, e.g.
`/tmp/dats-xxxxxx/test-<index>/inputs/<filename>`.

```yaml
inputs:
  files:
    data.txt: "content"
cmd: cat {inputs.data.txt}
```

### Output Placeholders

`{outputs.<filename>}` expands to a path under the test's `outputs/` directory where the
command should write output, e.g. `/tmp/dats-xxxxxx/test-<index>/outputs/<filename>`. The path
is provided; the command is responsible for creating the file. Every `{outputs.<filename>}`
resolves — the name does not need to appear under `outputs.files`.

```yaml
cmd: process -o {outputs.result.bin}
outputs:
  files:
    result.bin:
      exists: true
```

### Multiple Placeholders

```yaml
cmd: diff {inputs.a.txt} {inputs.b.txt} > {outputs.diff.txt}
```

### Placeholders in Input File Contents

The same expansion is applied to the contents of `inputs.files`, so a fixture (e.g. a script
or program the command runs) can reference other input paths and output paths:

```yaml
inputs:
  files:
    script.sh: 'cp {inputs.data.txt} {outputs.copy.txt}'
    data.txt: "content"
cmd: bash {inputs.script.sh}
outputs:
  files:
    copy.txt:
      match:
        - "content"
```

`{inputs.<name>}` for a name not declared under `inputs.files` is left untouched (in both the
command and file contents), as is any other brace construct.

---

## Exit Code Field (`exit`)

### Integer Values (0-255)

```yaml
exit: 0      # success
exit: 1      # generic failure
exit: 127    # command not found
```

### Variable Names

Must match pattern `^EXIT_[A-Z_]+$`. The runner recognizes:

- `EXIT_SUCCESS` = 0
- `EXIT_FAILURE` = 1

```yaml
exit: EXIT_SUCCESS
exit: EXIT_FAILURE
```

---

## Timeout Field (`timeout`)

Bounds how long the command may run. When the deadline elapses the command is killed and the
test fails with a "command timed out" message. Accepts either a bare integer number of seconds
or a Go duration string. `0` or an omitted field means no timeout.

```yaml
timeout: 5       # 5 seconds
timeout: 500ms   # 500 milliseconds
timeout: 1m30s   # 90 seconds
```

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

Content piped to the command's standard input.

```yaml
inputs:
  stdin: "hello world"
cmd: grep hello
```

### `files`

Map of filename to content. Each file is created before the test runs; reference it in the
command with `{inputs.<filename>}`. Nested paths (e.g. `sub/dir/file.txt`) are supported.
Contents go through `{inputs.X}`/`{outputs.X}` placeholder expansion — see
[Placeholders in Input File Contents](#placeholders-in-input-file-contents).

```yaml
inputs:
  files:
    config.json: |
      {"key": "value"}
    data.csv: "a,b,c"
```

---

## Outputs Block

```yaml
outputs:
  stdout:        # patterns that MUST appear (or line-number map)
  stderr:        # patterns that MUST appear (or line-number map)
  "!stdout":     # patterns that must NOT appear
  "!stderr":     # patterns that must NOT appear
  files:         # output file checks
  "!files":      # negated output file checks
```

### Pattern Lists

A list of substring patterns matched anywhere in the output:

```yaml
outputs:
  stdout:
    - "expected text"
    - "another pattern"
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

**Note**: You cannot mix pattern lists and line-specific checks in the same block. Use one
format or the other.

### Negated Output Checks

`!stdout` and `!stderr` assert substring patterns do NOT appear:

```yaml
outputs:
  "!stdout":
    - "error"
    - "failed"
  "!stderr":
    - "warning"
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
| `match` | string[] | Regex patterns that must match the file's contents |
| `notMatch` | string[] | Regex patterns that must NOT match the file's contents |

### Negated File Checks (`!files`)

`!files` accepts the same `exists`/`match`/`notMatch` fields as `files`. It is commonly used to
assert that a file must NOT exist:

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
    timeout: int|string    # optional, seconds or duration string; 0/omitted = no timeout
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
          match: []
          notMatch: []
```
