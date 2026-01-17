# Plan: DATS - Declarative Automated Testing System

## Goal
Create a Go-based declarative YAML-to-BATS test generation tool with VS Code extension support. File extension: `.dats` (valid YAML with custom schema).

## Reference Files (from ~/repos/ffs/impl/bash/spec/tests/)
| File | Purpose |
|------|---------|
| `generate-tests.sh` | Original bash converter (reference for behavior) |
| `test_helper.bash` | Runtime helper (will keep as bash) |
| `tests.schema.json` | JSON Schema (reference for YAML structure) |
| `test_helper/` | bats-support and bats-assert (copy as-is) |

## Design Decisions

### Command Execution
- **Original**: Hardcoded `$FFS <mode> <fixture_file>` pattern
- **New**: User specifies full `cmd` with `{inputs.X}` and `{outputs.X}` placeholders

### Input/Output Model
- **Original**: Single `code` field written to fixture file
- **New**: `inputs` block for multiple named files, `stdin` for piped input, `outputs` block for all validations

### Exit Codes
- Users define constants in test_helper.bash (e.g., `export EXIT_SYNTAX_ERROR=2`)
- Reference in YAML as `exit: EXIT_SYNTAX_ERROR` or numeric `exit: 2`
- Ship with EXIT_SUCCESS=0, EXIT_FAILURE=1 as defaults

## Proposed YAML Schema

```yaml
tests:
  # Simple command with no inputs
  - name: echo test
    exit: 0
    cmd: echo Hello World
    outputs:
      stdout:
        - "Hello World"

  # Command reading from file
  - name: cat reads file
    exit: 0
    inputs:
      input.txt: |
        Hello, world!
    cmd: cat {inputs.input.txt}
    outputs:
      stdout:
        - "Hello, world!"

  # Command reading from stdin
  - name: cat reads stdin
    exit: 0
    stdin: "Hello from stdin"
    cmd: cat
    outputs:
      stdout:
        - "Hello from stdin"

  # Multiple input files
  - name: diff two files
    exit: 1
    inputs:
      a.txt: "line1"
      b.txt: "line2"
    cmd: diff {inputs.a.txt} {inputs.b.txt}
    outputs:
      stdout:
        - "differ"

  # Output file validation
  - name: compiler produces binary
    exit: 0
    inputs:
      main.c: |
        int main() { return 0; }
    cmd: gcc -o {outputs.binary} {inputs.main.c}
    outputs:
      binary:
        exists: true
      stderr: []

  # Line-specific assertions (existing feature)
  - name: line matching
    exit: 0
    cmd: printf "line0\nline1\nline2"
    outputs:
      stdout:
        0: "^line0$"
        2: "^line2$"

  # Negative assertions (existing feature)
  - name: no errors
    exit: 0
    cmd: echo success
    outputs:
      stdout:
        - "success"
      "!stderr":
        - "error"
```

**Key elements:**
- `cmd` - the command to run, with `{inputs.X}` and `{outputs.X}` placeholders
- `inputs` - named files to create before running (content as value)
- `stdin` - content piped to command
- `outputs` - validation block containing:
  - `stdout` / `stderr` - pattern arrays or line-number maps
  - `!stdout` / `!stderr` - negative pattern assertions
  - Named files - for output file validation (exists, content patterns, etc.)

## Target Directory Structure

```
~/repos/bats_declarative/
├── cmd/
│   └── dats/
│       └── main.go           # CLI entry point
├── internal/
│   ├── generator/
│   │   └── generator.go      # Core DATS-to-BATS logic
│   └── schema/
│       └── types.go          # YAML schema types
├── runtime/                  # Files for test execution
│   ├── test_helper.bash      # Runtime helper
│   └── test_helper/          # bats-support and bats-assert
│       ├── bats-support/
│       └── bats-assert/
├── vscode-dats/              # VS Code extension
│   ├── package.json          # Extension manifest
│   ├── language-configuration.json
│   ├── syntaxes/
│   │   └── dats.tmLanguage.json   # TextMate grammar
│   └── snippets/
│       └── dats.json         # Code snippets
├── schema.json               # JSON Schema for validation
├── go.mod
├── go.sum
├── Makefile
└── examples/
    ├── example.dats          # Example test file
    └── Makefile
```

## Implementation Steps

1. **Initialize Go module**
   - `go mod init github.com/mhaynie/bats-declarative` (or appropriate path)
   - Add gopkg.in/yaml.v3 dependency

2. **Create YAML schema types** (`internal/schema/types.go`):
   ```go
   type TestFile struct {
       Tests []Test `yaml:"tests"`
   }
   type Test struct {
       Name    string            `yaml:"name"`
       Exit    interface{}       `yaml:"exit"`    // int or "EXIT_*" string
       Cmd     string            `yaml:"cmd"`     // command with {inputs.X}/{outputs.X} placeholders
       Stdin   string            `yaml:"stdin"`   // optional stdin content
       Inputs  map[string]string `yaml:"inputs"`  // filename -> content
       Outputs OutputBlock       `yaml:"outputs"` // stdout/stderr/files
   }
   type OutputBlock struct {
       Stdout    OutputCheck            `yaml:"stdout"`
       Stderr    OutputCheck            `yaml:"stderr"`
       NotStdout OutputCheck            `yaml:"!stdout"`
       NotStderr OutputCheck            `yaml:"!stderr"`
       Files     map[string]FileCheck   `yaml:"-"`  // other keys = output files
   }
   type OutputCheck interface{}  // []string (patterns) or map[int]string (line checks)
   type FileCheck struct {
       Exists   bool        `yaml:"exists"`
       Contains []string    `yaml:"contains"`  // patterns
   }
   ```

3. **Create generator** (`internal/generator/generator.go`):
   - Parse YAML file
   - For each test:
     - Create input files from `inputs` block
     - Expand `{inputs.X}` and `{outputs.X}` placeholders in `cmd`
     - Generate bats test with:
       - `run` or stdin piping based on `stdin` field
       - Exit code assertion
       - stdout/stderr assertions from `outputs` block
       - Output file existence/content checks
   - Output dependency .d file for Make integration

4. **Create CLI** (`cmd/dats/main.go`):
   - Usage: `dats <file.dats> [output_dir]`
   - Generates .gen.bats files and input fixtures

5. **Copy runtime files**:
   - test_helper.bash (simplified, generic exit codes)
   - bats-support/ and bats-assert/ directories

6. **Create schema.json** (for IDE validation):
   - `cmd` - required string
   - `exit` - int (0-255) or string matching `EXIT_*`
   - `inputs` - optional map of filename -> content
   - `stdin` - optional string
   - `outputs` - object with stdout/stderr/!stdout/!stderr + arbitrary file keys

7. **Create VS Code extension** (`vscode-dats/`):
   - `package.json` - extension manifest, contributes language, schema association
   - `language-configuration.json` - bracket matching, comment toggling
   - `syntaxes/dats.tmLanguage.json` - TextMate grammar extending YAML
   - `snippets/dats.json` - snippets for test, inputs, outputs blocks
   - Associate `.dats` files with YAML + JSON schema

8. **Create example** `.dats` file and Makefile

## Verification
- Build the Go binary: `go build ./cmd/dats`
- Create example.dats testing `echo` or `cat`
- Generate: `./dats examples/example.dats examples/`
- Run tests: `bats examples/*.gen.bats`
- Test VS Code extension: Open `.dats` file, verify syntax highlighting and schema validation

## Critical Files to Create
- `/Users/mhaynie/repos/bats_declarative/cmd/dats/main.go`
- `/Users/mhaynie/repos/bats_declarative/internal/generator/generator.go`
- `/Users/mhaynie/repos/bats_declarative/internal/schema/types.go`
- `/Users/mhaynie/repos/bats_declarative/runtime/test_helper.bash`
- `/Users/mhaynie/repos/bats_declarative/schema.json`
- `/Users/mhaynie/repos/bats_declarative/go.mod`
- `/Users/mhaynie/repos/bats_declarative/vscode-dats/package.json`
- `/Users/mhaynie/repos/bats_declarative/vscode-dats/syntaxes/dats.tmLanguage.json`
