# Examples

This page shows real examples from `examples/example.dats` in the DATS repository.

## Complete Example File

Here is the full `examples/example.dats` file:

```yaml
tests:
  # Simple command with no inputs
  - desc: echo test
    exit: 0
    cmd: echo Hello World
    outputs:
      stdout:
        - "Hello World"

  # Command reading from file
  - desc: cat reads file
    exit: 0
    inputs:
      files:
        input.txt: |
          Hello, world!
    cmd: cat {inputs.input.txt}
    outputs:
      stdout:
        - "Hello, world!"

  # Command reading from stdin
  - desc: cat reads stdin
    exit: 0
    inputs:
      stdin: "Hello from stdin"
    cmd: cat
    outputs:
      stdout:
        - "Hello from stdin"

  # Multiple input files
  - desc: concatenate two files
    exit: 0
    inputs:
      files:
        a.txt: "Line A"
        b.txt: "Line B"
    cmd: cat {inputs.a.txt} {inputs.b.txt} {inputs.a.txt}
    outputs:
      stdout:
        - "Line A"
        - "Line B"

  # Line-specific assertions
  - desc: line matching
    exit: 0
    cmd: printf "line0\nline1\nline2"
    outputs:
      stdout:
        0: "^line0$"
        2: "^line2$"

  # Negative assertions
  - desc: no errors in output
    exit: 0
    cmd: echo success
    outputs:
      stdout:
        - "success"
      "!stdout":
        - "error"
        - "fail"

  # Expected non-zero exit
  - desc: grep returns 1 when not found
    exit: 1
    inputs:
      stdin: "hello world"
    cmd: grep -q "notfound"

  # Using EXIT_* variable
  - desc: exit code variable
    exit: EXIT_SUCCESS
    cmd: true
```

## Running the Examples

```bash
dats examples/example.dats examples/
```

Output:
```
Generated: /path/to/examples/example.gen.bats
Created 3 fixture file(s)
Running: bats /path/to/examples/example.gen.bats
1..8
ok 1 echo test
ok 2 cat reads file
ok 3 cat reads stdin
ok 4 concatenate two files
ok 5 line matching
ok 6 no errors in output
ok 7 grep returns 1 when not found
ok 8 exit code variable
```

## Pattern Breakdown

### Simple Command

```yaml
- desc: echo test
  cmd: echo Hello World
  outputs:
    stdout:
      - "Hello World"
```

Exit code defaults to 0 if not specified.

### File Inputs

```yaml
- desc: cat reads file
  inputs:
    files:
      input.txt: |
        Hello, world!
  cmd: cat {inputs.input.txt}
```

The `{inputs.input.txt}` placeholder expands to the fixture file path.

### Stdin Input

```yaml
- desc: cat reads stdin
  inputs:
    stdin: "Hello from stdin"
  cmd: cat
```

### Line-Specific Assertions

```yaml
- desc: line matching
  cmd: printf "line0\nline1\nline2"
  outputs:
    stdout:
      0: "^line0$"
      2: "^line2$"
```

Integer keys (0, 2) specify line numbers. Values are regex patterns.

### Negative Assertions

```yaml
- desc: no errors in output
  cmd: echo success
  outputs:
    stdout:
      - "success"
    "!stdout":
      - "error"
      - "fail"
```

`!stdout` patterns must NOT appear in the output.

### Exit Code Variables

```yaml
- desc: exit code variable
  exit: EXIT_SUCCESS
  cmd: true
```

Use `EXIT_SUCCESS` or `EXIT_FAILURE` for readable test definitions.
