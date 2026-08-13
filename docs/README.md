# DATS Documentation

DATS (Declarative Automated Testing System) runs tests defined in `.dats` YAML files. It
natively executes each command, captures the exit code and output, and verifies assertions
without requiring any external test framework.

## Documentation Index

- [File Format Reference](file-format.md) - Complete `.dats` YAML schema
- [CLI Usage](cli.md) - Command-line interface
- [Go Library API](library.md) - Running suites in-process from Go
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

- **Tests** are defined in YAML with a simple, declarative format — indented with tabs, not
  spaces; see [YAML Dialect](file-format.md#yaml-dialect)
- **Placeholders** like `{inputs.file.txt}` and `{outputs.result.txt}` expand to absolute
  paths inside a per-run temp directory
- **Exit codes** can be integers (0-255) or variables like `EXIT_SUCCESS`
- **Timeouts** (`timeout`) bound how long a command may run
- **Environment variables** (`inputs.env`) are added to the command's inherited environment
- **Output checks** match patterns or specific lines in stdout/stderr
- **Negated checks** (`!stdout`, `!stderr`, `!files`) assert patterns/files do NOT appear
- **Snapshots** (`snapshot`) byte-compare stdout/stderr against golden files stored next to
  the `.dats` file; `dats --update` rewrites them from actual output
- **Sandboxing** is on by default (bubblewrap, falling back to docker): commands may only
  write inside their temp directory. `--no-sandbox` is the only opt-out, and it belongs to
  whoever runs the file — a `.dats` file can narrow its own sandbox but never switch it off
- **Copy fixtures** (`inputs.copy`, `shared.copy`) pull an existing host file into that
  writable temp directory — the read-write counterpart of the sandbox's read-only bind mount
  of the working directory. Heredocs and herestrings in `cmd`/`setup`/`teardown` are rejected
  at parse time: write the file and pull it in with `files`/`copy`, or use `inputs.stdin`/a
  pipe, instead
