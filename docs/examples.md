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

### Bounding Run Time

`timeout` accepts an integer number of seconds or a Go duration string. A command that exceeds
the timeout is killed and the test fails.

```yaml
tests:
	- desc: must finish quickly
	  cmd: ./fast-tool
	  timeout: 2s
	  outputs:
		stdout:
			- "done"
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

### Environment Variables

`inputs.env` adds variables to the environment the command inherits. Values go through the
same placeholder expansion as the command, so a variable can point at a fixture:

```yaml
tests:
	- desc: tool reads config path from the environment
	  cmd: 'echo "$APP_MODE"; cat "$CONFIG_PATH"'
	  inputs:
		files:
			config.json: |
				{"mode": "test"}
		env:
			APP_MODE: test
			CONFIG_PATH: "{inputs.config.json}"
	  outputs:
		stdout:
			- "test"
			- '"mode": "test"'
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
		!stdout:
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
		!files:
			error.log:
				exists: true    # inverted: error.log must NOT exist
```

---

## Snapshot (Golden-File) Assertions

Pin an entire output verbatim without spelling it out in YAML: the captured stream must
byte-match a golden file stored next to the `.dats` file. Create and refresh goldens with
`dats --update`; see the
[file format reference](file-format.md#snapshot-assertions-outputssnapshot) for storage,
naming, and normalization details, and `examples/snapshot.dats` in the repository for a
runnable demo with committed goldens.

```yaml
tests:
	- desc: renders the report          # golden: <file>.snapshots/001-renders-the-report.stdout.golden
	  cmd: mytool report
	  outputs:
		snapshot: true                  # boolean shorthand: snapshot stdout

	- desc: split streams
	  cmd: mytool report --warnings
	  outputs:
		snapshot:                       # per-stream form
			stdout: true
			stderr: true
```

---

## Sandboxing

Test commands are sandboxed by default, and ordinary tests need no changes for it: fixtures,
`{outputs.X}` and `{shared.X}` all live in the sandbox's writable temp directory.

### Narrowing the sandbox for one file

```yaml
sandbox:
	network: false        # these commands need no network, so they get none
	image: golang:1.25    # ...and the docker backend needs a Go in the image
tests:
	- desc: builds offline
	  cmd: go build ./...
```

Scratch space is the temp directory, never a host path: there is no way to declare a host
path writable. A command that must write outside it is a `sandbox: false` file.

### Pulling a host file in, writable

`inputs.copy`/`shared.copy` copy an *existing* host file into that writable temp directory --
the read-write counterpart of the working directory's read-only bind mount, for a fixture the
test needs to mutate:

```yaml
tests:
	- desc: modifies a copied-in fixture
	  inputs:
		copy:
			config.json: fixtures/config.json   # path relative to this .dats file
	  cmd: echo patched >> {inputs.config.json}; cat {inputs.config.json}
```

See [file-format.md](file-format.md#copy-fixtures-inputscopy-and-sharedcopy) for the full
reference, including why heredocs and herestrings are rejected in `cmd`/`setup`/`teardown` in
favor of this, `files`, and `inputs.stdin`.

### Opting a file out

For commands that genuinely need the host -- driving the local docker daemon, installing
packages, writing outside the temp tree:

```yaml
sandbox: false
tests:
	- desc: the docker daemon is reachable
	  cmd: docker info
```

### Picking the image for the docker backend

Only used when the docker backend is selected (bubblewrap runs commands against the host's
own filesystem and ignores this). The image must ship bash:

```yaml
sandbox:
	image: golang:1.24
tests:
	- desc: builds with the toolchain from the image
	  cmd: go build -o {outputs.bin} ./...
```

See [cli.md](cli.md#sandboxing---sandbox) for what each backend isolates and how to opt a
whole run out (`--no-sandbox`).

## Common Patterns

### Testing CLI Tools

Pattern lists are literal substrings, so matching a version number takes the line-keyed regex
form (list patterns like `v[0-9]+` would be searched for verbatim and never match):

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
			0: "v[0-9]+\\.[0-9]+"
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
		!stdout:
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
		!stderr:
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
		!stdout:
			- "Compiled successfully"

	# Output file generation
	- desc: generates binary output
	  exit: 0
	  cmd: mycompiler {inputs.source.lang} -o {outputs.binary}
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
			- "Usage: mycompiler"
			- "--help"
			- "--version"
			- "--output"

	- desc: shows version
	  exit: 0
	  cmd: mycompiler --version
	  outputs:
		stdout:
			# line-keyed regex form: list patterns are literal substrings and
			# could never match a version pattern like this
			0: "^mycompiler v[0-9]+\\.[0-9]+\\.[0-9]+$"
```
