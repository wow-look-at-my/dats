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

  # Stderr assertions (cmd is quoted because it contains a colon)
  - desc: writes to stderr
    exit: 0
    cmd: 'echo "warning: be careful" >&2'
    outputs:
      stderr:
        - "warning"
      "!stderr":
        - "panic"

  # Output file validation
  - desc: writes an output file
    exit: 0
    cmd: echo "result data" > {outputs.result.txt}
    outputs:
      files:
        result.txt:
          exists: true
          match:
            - "result data"
          notMatch:
            - "error"

  # Negated output file: a stray file must NOT be created
  # (!files inverts each check, so exists: true means "must NOT exist")
  - desc: does not create a stray file
    exit: 0
    cmd: echo nothing
    outputs:
      "!files":
        unexpected.txt:
          exists: true

  # Per-test timeout (command completes well within the limit)
  - desc: finishes within timeout
    exit: 0
    cmd: echo fast
    timeout: 5s
    outputs:
      stdout:
        - "fast"

  # Per-test environment variables, added to the inherited environment.
  # Values go through the same placeholder expansion as the command.
  - desc: environment variables are visible to the command
    exit: 0
    inputs:
      files:
        cfg.json: '{"mode": "test"}'
      env:
        GREETING: hello from env
        CONFIG_PATH: "{inputs.cfg.json}"
    cmd: echo "$GREETING"; cat "$CONFIG_PATH"
    outputs:
      stdout:
        - "hello from env"
        - '"mode": "test"'

  # Nested output file: parent directories of outputs declared under
  # files/!files are created before the command runs
  - desc: writes a nested output file
    exit: 0
    cmd: echo "nested report" > {outputs.sub/report.txt}
    outputs:
      files:
        sub/report.txt:
          match:
            - "nested report"

  # An empty file check is an implicit existence assertion (must exist)
  - desc: empty check means the file must exist
    exit: 0
    cmd: date > {outputs.stamp.txt}
    outputs:
      files:
        stamp.txt: {}
