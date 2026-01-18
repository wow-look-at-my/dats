# CLI Usage

## Synopsis

```
dats <file.dats> [output_dir] [--runtime-dir=<path>]
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

### `--runtime-dir=<path>` (optional)

Path to the runtime directory containing `test_helper.bash`.

The runtime directory is discovered in this order:
1. Explicit `--runtime-dir` argument
2. `./runtime` relative to current working directory
3. Alongside the dats binary
4. One level up from the binary (for `bin/dats` layouts)

## Examples

### Basic Usage

```bash
# Generate test.gen.bats in same directory as test.dats
dats test.dats

# Generate in a specific output directory
dats test.dats ./generated/

# Specify runtime directory explicitly
dats test.dats ./generated/ --runtime-dir=/path/to/runtime
```

### Help

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

### Dependency File

The `.gen.bats.d` file lists all dependencies for Make-based build systems:

```makefile
/path/to/example.gen.bats: /path/to/example.dats /path/to/fixtures/example/1/inputs/input.txt ...
```

This enables incremental rebuilds when source files change.

## Integration with BATS

After generating, run tests with BATS:

```bash
# Run a single test file
bats example.gen.bats

# Run all generated tests
bats *.gen.bats

# Run with verbose output
bats --verbose-run example.gen.bats
```

## Build System Integration

### Just

```just
test:
    dats tests/suite.dats tests/
    bats tests/suite.gen.bats
```

### Make

```makefile
%.gen.bats: %.dats
    dats $< $(dir $<)

-include $(wildcard *.gen.bats.d)
```
