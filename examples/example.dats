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
      input.txt: |
        Hello, world!
    cmd: cat {inputs.input.txt}
    outputs:
      stdout:
        - "Hello, world!"

  # Command reading from stdin
  - desc: cat reads stdin
    exit: 0
    stdin: "Hello from stdin"
    cmd: cat
    outputs:
      stdout:
        - "Hello from stdin"

  # Multiple input files
  - desc: concatenate two files
    exit: 0
    inputs:
      a.txt: "Line A"
      b.txt: "Line B"
    cmd: cat {inputs.a.txt} {inputs.b.txt} {inputs.a.txt}
    stdin:
      
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
    stdin: "hello world"
    cmd: grep -q "notfound"

  # Using EXIT_* variable
  - desc: exit code variable
    exit: EXIT_SUCCESS
    cmd: true
