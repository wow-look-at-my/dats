# DATS Documentation

DATS (Declarative Automated Testing System) runs tests defined in `.dats` YAML files. It
natively executes each command, captures the exit code and output, and verifies assertions
without requiring any external test framework.

## Documentation Index

- [File Format Reference](file-format.md) - Complete `.dats` YAML schema
- [CLI Usage](cli.md) - Command-line interface
- [Machine-Readable Reports](reports.md) - JUnit XML / JSON report file formats
- [Examples](examples.md) - Annotated examples

## Quick Start

1. Create a `.dats` file:

```yaml
tests:
  - desc: hello world
    cmd: echo "Hello, World!"
    outputs:
      stdout:
        - "Hello, World!"
```

2. Run it:

```bash
dats mytest.dats
```

Results are printed in a TAP-like format and the process exits non-zero if any test fails.

## Key Concepts

- **Tests** are defined in YAML with a simple, declarative format
- **Placeholders** like `{inputs.file.txt}` and `{outputs.result.txt}` expand to absolute
  paths inside a per-run temp directory
- **Exit codes** can be integers (0-255) or variables like `EXIT_SUCCESS`
- **Timeouts** (`timeout`) bound how long a command may run
- **Environment variables** (`inputs.env`) are added to the command's inherited environment
- **Output checks** match patterns or specific lines in stdout/stderr
- **Negated checks** (`!stdout`, `!stderr`, `!files`) assert patterns/files do NOT appear
