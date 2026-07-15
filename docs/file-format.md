# DATS File Format Reference

## Root Structure

A `.dats` file contains a single `tests` array:

```yaml
tests:
  - # test 1
  - # test 2
```

A file must contain exactly one YAML document. A second `---` document is a parse error
(`multiple YAML documents are not supported`) rather than being silently dropped. Unknown keys
anywhere in the file are also parse errors, so a misspelled field cannot silently disable its
assertion.

## Test Object

Each test has these fields:

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `cmd` | string | Yes | - | Command to execute |
| `desc` | string | No | Value of `cmd` | Test description/name |
| `exit` | int or string | No | `0` | Expected exit code |
| `timeout` | int or string | No | none | Per-test timeout (seconds or duration string) |
| `inputs` | object | No | - | Stdin, input files, and environment variables |
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
      env:
        PROCESS_MODE: strict
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

The command is run with `bash -c` in the working directory of the `dats` invocation (the
runner does not change directory). Fixture files live in a fresh per-run temp directory and
are addressed by absolute path through placeholders:

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
is provided; the command is responsible for creating the file. The name does not need to
appear under `outputs.files` — any **local relative** name resolves. A non-local name (one
containing `..` or an absolute path, e.g. `{outputs.../escape}`) is left verbatim, so a
placeholder can never address a path outside the test directory.

Names that DO appear under `files`/`!files` get their parent directories created before the
command runs, so a command can write a declared nested output like
`{outputs.sub/report.txt}` directly. For undeclared nested names, the command must create the
intermediate directories itself.

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

Bare and quoted integers are equivalent; the 0-255 range is enforced either way:

```yaml
exit: 0      # success
exit: 1      # generic failure
exit: 127    # command not found
exit: "3"    # quoted integer, same as 3
```

### Variable Names

Exactly two names are recognized (any other `EXIT_*` name is rejected at parse time, since the
runner could never resolve it):

- `EXIT_SUCCESS` = 0
- `EXIT_FAILURE` = 1

```yaml
exit: EXIT_SUCCESS
exit: EXIT_FAILURE
```

### Signal Deaths

A command terminated by a signal has no normal exit code; it surfaces as `-1` with the signal
named in the failure message:

```
# expected exit code 0, got -1 (killed by signal: killed)
```

---

## Timeout Field (`timeout`)

Bounds how long the command may run. Accepts an integer number of seconds (bare or quoted — a
quoted bare integer like `"5"` also means seconds) or a Go duration string. `0` or an omitted
field means no timeout.

```yaml
timeout: 5       # 5 seconds
timeout: "5"     # quoted integer, also 5 seconds
timeout: 500ms   # 500 milliseconds
timeout: 1m30s   # 90 seconds
```

When the deadline elapses, the command's **whole process group** is killed — background
children included, not just the direct `bash` process. The test then fails with only a
`command timed out after <duration>` message; every other assertion is skipped, since checking
partial output or missing files would bury the real cause under secondary failures. The
stdout/stderr captured before the kill are still shown in verbose mode.

A command that leaves orphaned background processes holding its stdout/stderr open cannot
block the runner indefinitely: the pipes are force-closed about 1 second after the main
process exits (timeout or not). Output written by such stragglers after that point is not
captured.

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
  env:
    MY_VAR: "value"
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

File names must be relative paths that stay inside the test directory — `..` traversal and
absolute paths are rejected at parse time (so `dats syntax` catches them) and again at
fixture-setup time. The same rule applies to the names under `outputs.files` and
`outputs.!files`.

```yaml
inputs:
  files:
    config.json: |
      {"key": "value"}
    data.csv: "a,b,c"
```

### `env`

Map of environment variable name to value. The variables are **added** to the environment the
command inherits from `dats` (they do not replace it), appended in sorted key order so runs
are deterministic. Values go through the same `{inputs.X}`/`{outputs.X}` placeholder expansion
as the command, so a variable can carry a fixture's absolute path:

```yaml
inputs:
  files:
    cfg.json: '{"mode": "test"}'
  env:
    MY_VAR: hello
    CONFIG_PATH: "{inputs.cfg.json}"
cmd: 'echo "$MY_VAR"; cat "$CONFIG_PATH"'
```

With `--coverdir`, `GOCOVERDIR` is appended after the test's variables, so the flag wins even
over a test's own `GOCOVERDIR` entry.

---

## Outputs Block

```yaml
outputs:
  stdout:        # patterns that MUST appear (or line-number map)
  stderr:        # patterns that MUST appear (or line-number map)
  "!stdout":     # patterns that must NOT appear (or line-number map)
  "!stderr":     # patterns that must NOT appear (or line-number map)
  files:         # output file checks
  "!files":      # negated output file checks
  json_output:   # expected JSON value of the whole stdout
```

### Pattern Lists

A list of **literal substrings**, each of which must appear somewhere in the output. They are
not regexes — metacharacters have no special meaning:

```yaml
outputs:
  stdout:
    - "expected text"
    - "another pattern"
```

### Line-Specific Checks

Use integer keys (0-indexed) with Go/RE2 regex values. Each regex is searched **unanchored**
within its line — use `^...$` to pin the whole line. Addressing a line the output does not
have fails the test (empty output has zero lines):

```yaml
outputs:
  stdout:
    0: "^first line$"
    2: "^third line$"
    5: "pattern on line 6"
```

Line keys must be unique, non-negative integers: a duplicate line number (including different
spellings such as `0` and `00`) and a negative line number are both parse errors.

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

Like the positive checks, negated checks also accept the line-specific map form. Each regex
must NOT match (unanchored search) within the given 0-indexed line. A line that does not
exist passes — there is nothing there to match:

```yaml
outputs:
  "!stdout":
    0: "error"        # line 0 must not contain "error"
    2: "^warning"     # line 2 must not start with "warning" (also passes if there is no line 2)
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

File names must be relative paths that stay inside the test directory (`..` and absolute
paths are rejected at parse time). Nested names like `sub/report.txt` are allowed; parent
directories of every file declared under `files`/`!files` are created before the command
runs. When several file checks fail, the failures are reported in sorted-by-name order.

### Fields

| Field | Type | Description |
|-------|------|-------------|
| `exists` | boolean | Whether the file should exist |
| `match` | string[] | Regex patterns that must match the file's contents |
| `notMatch` | string[] | Regex patterns that must NOT match the file's contents |

### Empty Checks Are Implicit Existence Assertions

A check with none of the three fields — written `{}` or left null — asserts existence rather
than passing vacuously. Under `files` the file must exist; under `!files` it must NOT exist:

```yaml
outputs:
  files:
    out.txt: {}       # out.txt must exist
    log.txt:          # null value, same meaning: log.txt must exist
  "!files":
    stray.txt: {}     # stray.txt must NOT exist
```

### Negated File Checks (`!files`)

`!files` accepts the same `exists`/`match`/`notMatch` fields as `files`, but asserts the
NEGATION of each check:

| Field | `files` meaning | `!files` meaning |
|-------|-----------------|------------------|
| `exists: true` | file must exist | file must NOT exist |
| `exists: false` | file must NOT exist | file must exist |
| `match: [p]` | contents must match `p` | contents must NOT match `p` (a missing file passes) |
| `notMatch: [p]` | contents must NOT match `p` (a missing file passes) | contents must match `p` (a missing file fails) |

The common use is asserting that a file must NOT exist or must NOT contain something:

```yaml
outputs:
  "!files":
    error.log:
      exists: true        # error.log must NOT exist
    report.txt:
      match:
        - "FAILED"        # report.txt must NOT contain FAILED
```

---

## JSON Output (`json_output`)

`json_output` asserts that the whole stdout is a single JSON value that deep-equals the given
value:

```yaml
tests:
  - desc: emits the expected JSON document
    cmd: mytool --json
    outputs:
      json_output:
        name: dats
        count: 2
        tags: [a, b]
```

Comparison rules:

- Object keys are **order-insensitive**; arrays are **order-sensitive**.
- Numbers compare by value (`2` equals `2.0`).
- The expected value may be any JSON value — object, array, string, number, bool, or `null`
  (`json_output: null` asserts stdout is exactly the JSON `null`).
- Stdout must contain exactly one JSON value (trailing whitespace is fine, trailing data is
  not). Non-JSON stdout fails the assertion.

On mismatch the failure lists each difference with its JSONPath-style location:

```
# json_output: at $.tokens[3].kind: expected "Ident", got "Keyword"
# json_output: at $: missing key "eof" (expected true)
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
      env:                 # optional
        <name>: string     # env var: value (placeholders expanded)
    outputs:
      stdout: []|{}        # pattern list or line checks
      stderr: []|{}        # pattern list or line checks
      "!stdout": []|{}     # negated patterns
      "!stderr": []|{}     # negated patterns
      files:
        <name>:            # empty check ({}/null) = must exist
          exists: bool
          match: []
          notMatch: []
      "!files":
        <name>:            # empty check ({}/null) = must NOT exist
          exists: bool
          match: []
          notMatch: []
      json_output: any     # expected JSON value of the whole stdout
```
