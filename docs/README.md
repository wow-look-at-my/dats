# DATS Documentation

DATS (Declarative Automated Testing System) converts `.dats` YAML files into BATS (Bash Automated Testing System) test files and runs them.

## Documentation Index

- [File Format Reference](file-format.md) - Complete .dats YAML schema
- [CLI Usage](cli.md) - Command-line interface
- [Examples](examples.md) - Annotated examples
- [Generated Output](generated-output.md) - Understanding the generated BATS files
- [Runtime Helpers](runtime.md) - Available assertion functions

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

2. Run with dats:

```bash
dats mytest.dats
```

That's it! DATS generates the BATS test file and runs it automatically.

## Key Concepts

- **Tests** are defined in YAML with a simple, declarative format
- **Placeholders** like `{inputs.file.txt}` reference fixture files
- **Exit codes** can be integers (0-255) or variables like `EXIT_SUCCESS`
- **Output checks** match patterns or specific lines in stdout/stderr
- **Negated checks** (`!stdout`, `!stderr`) assert patterns do NOT appear
