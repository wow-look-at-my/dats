# CLI Usage

## Synopsis

```
dats <file.dats> [output_dir]
```

## Arguments

### `file.dats` (required)

Path to the `.dats` input file. Must have `.dats` extension.

### `output_dir` (optional)

Directory where generated files are written. Defaults to the same directory as the input file.

Generated files:
- `<basename>.gen.bats` - The BATS test file
- `<basename>.gen.bats.d` - Make dependency file
- `fixtures/<basename>/` - Input fixture files (if tests define inputs)

## Examples

```bash
# Generate and run tests (output in same directory as input)
dats test.dats

# Generate and run tests in a specific output directory
dats test.dats ./generated/
```

## Help

```bash
dats -h
dats --help
```

## Output Structure

Given `examples/example.dats`:

```
examples/
  example.dats              # input
  example.gen.bats          # generated BATS file
  example.gen.bats.d        # Make dependency file
  fixtures/
    example/
      1/
        inputs/
          input.txt         # fixture from test index 1
      3/
        inputs/
          a.txt             # fixtures from test index 3
          b.txt
```

## Dependency File

The `.gen.bats.d` file lists all dependencies for Make-based build systems:

```makefile
/path/to/example.gen.bats: /path/to/example.dats /path/to/fixtures/example/1/inputs/input.txt ...
```

This enables incremental rebuilds when source files change.

## Exit Codes

DATS passes through the exit code from BATS:
- `0` - All tests passed
- `1` - One or more tests failed
