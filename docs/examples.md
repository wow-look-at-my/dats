# Examples

## Basic Command Testing

### Simple Command

```yaml
tests:
  - desc: echo outputs hello
    cmd: echo hello
    outputs:
      stdout:
        - "hello"
```

### Command with Exit Code

```yaml
tests:
  - desc: false returns 1
    exit: 1
    cmd: false
```

### Using Exit Variables

```yaml
tests:
  - desc: true returns success
    exit: EXIT_SUCCESS
    cmd: true

  - desc: false returns failure
    exit: EXIT_FAILURE
    cmd: false
```

---

## Input Handling

### Stdin Input

```yaml
tests:
  - desc: grep finds pattern in stdin
    cmd: grep hello
    inputs:
      stdin: "hello world"
    outputs:
      stdout:
        - "hello world"
```

### File Input

```yaml
tests:
  - desc: cat reads file
    cmd: cat {inputs.data.txt}
    inputs:
      files:
        data.txt: "file content"
    outputs:
      stdout:
        - "file content"
```

### Multiple Files

```yaml
tests:
  - desc: diff two files
    exit: 1
    cmd: diff {inputs.a.txt} {inputs.b.txt}
    inputs:
      files:
        a.txt: "line 1"
        b.txt: "line 2"
```

### Combined Stdin and Files

```yaml
tests:
  - desc: process with config
    cmd: process --config {inputs.config.json}
    inputs:
      stdin: "input data"
      files:
        config.json: |
          {"mode": "test"}
```

---

## Output Validation

### Pattern Matching

```yaml
tests:
  - desc: output contains expected patterns
    cmd: echo "Hello, World! Status: OK"
    outputs:
      stdout:
        - "Hello"
        - "World"
        - "OK"
```

### Line-Specific Assertions

```yaml
tests:
  - desc: validate specific lines
    cmd: printf "header\ndata line\nfooter"
    outputs:
      stdout:
        0: "^header$"
        1: "^data"
        2: "footer$"
```

### Negated Assertions

```yaml
tests:
  - desc: no errors in output
    cmd: echo "success"
    outputs:
      stdout:
        - "success"
      "!stdout":
        - "error"
        - "fail"
        - "exception"
```

---

## File Output Validation

### Check File Exists

```yaml
tests:
  - desc: command creates output file
    cmd: touch {outputs.result.txt}
    outputs:
      files:
        result.txt:
          exists: true
```

### Check File Content

```yaml
tests:
  - desc: command writes expected content
    cmd: echo "data" > {outputs.out.txt}
    outputs:
      files:
        out.txt:
          exists: true
          match:
            - "^data$"
```

### Negative File Checks

```yaml
tests:
  - desc: command does not create error log
    cmd: process --quiet
    outputs:
      "!files":
        error.log:
          exists: false
```

---

## Common Patterns

### Testing CLI Tools

```yaml
tests:
  - desc: help flag works
    exit: 0
    cmd: mytool --help
    outputs:
      stdout:
        - "Usage:"
        - "--help"

  - desc: version flag works
    exit: 0
    cmd: mytool --version
    outputs:
      stdout:
        - "v[0-9]+\\.[0-9]+"
```

### Testing Error Handling

```yaml
tests:
  - desc: invalid input returns error
    exit: 1
    cmd: mytool --bad-flag
    outputs:
      stderr:
        - "unknown flag"
      "!stdout":
        - "success"
```

### Testing File Transformations

```yaml
tests:
  - desc: json to yaml conversion
    cmd: convert {inputs.data.json} -o {outputs.data.yaml}
    inputs:
      files:
        data.json: |
          {"key": "value"}
    outputs:
      files:
        data.yaml:
          exists: true
          match:
            - "key: value"
```

### Testing Pipelines

```yaml
tests:
  - desc: pipeline processes data
    cmd: cat {inputs.data.txt} | grep pattern | wc -l
    inputs:
      files:
        data.txt: |
          pattern match 1
          no match here
          pattern match 2
    outputs:
      stdout:
        - "2"
```

---

## Complete Real-World Example

```yaml
tests:
  # Basic functionality
  - desc: processes valid input
    exit: 0
    cmd: mycompiler {inputs.source.lang}
    inputs:
      files:
        source.lang: |
          function main() {
            print("Hello")
          }
    outputs:
      stdout:
        - "Compiled successfully"
      "!stderr":
        - "error"
        - "warning"

  # Error handling
  - desc: syntax error reports line number
    exit: 1
    cmd: mycompiler {inputs.bad.lang}
    inputs:
      files:
        bad.lang: |
          function main( {
            broken
          }
    outputs:
      stderr:
        - "syntax error"
        - "line 1"
      "!stdout":
        - "Compiled successfully"

  # Output file generation
  - desc: generates binary output
    exit: 0
    cmd: mycompiler {inputs.source.lang} -o {outputs/binary}
    inputs:
      files:
        source.lang: "print(42)"
    outputs:
      files:
        binary:
          exists: true

  # Help and version
  - desc: shows help
    exit: 0
    cmd: mycompiler --help
    outputs:
      stdout:
        0: "^Usage: mycompiler"
        - "--help"
        - "--version"
        - "--output"

  - desc: shows version
    exit: 0
    cmd: mycompiler --version
    outputs:
      stdout:
        - "^mycompiler v[0-9]+\\.[0-9]+\\.[0-9]+$"
```
