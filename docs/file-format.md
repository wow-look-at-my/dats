# DATS File Format Reference

## Root Structure

A `.dats` file contains a `tests` array, optionally preceded by the file-level `shared`,
`setup`, `teardown`, and `sandbox` keys:

```yaml
shared:      # optional file-level fixture files
setup:       # optional command(s) run once before the tests
teardown:    # optional command(s) always run once after the tests
sandbox:     # optional: narrow or opt out of the sandbox for this file
tests:
  - # test 1
  - # test 2
```

A file must contain exactly one YAML document. A second `---` document is a parse error
(`multiple YAML documents are not supported`) rather than being silently dropped. Unknown keys
anywhere in the file are also parse errors, so a misspelled field cannot silently disable its
assertion.

## File-Level Setup, Teardown, and Shared Fixtures

Three optional top-level keys run commands and materialize fixture files once per **file**
(backwards compatible for existing files — see
[Backwards Compatibility](#backwards-compatibility) for the one caveat):

```yaml
shared:
  files:
    config.json: |
      {"debug": true}

setup: cat {shared.config.json} > {shared.generated.txt}   # a single string...

teardown:                                                  # ...or a list of strings
  - first cleanup command
  - second cleanup command

tests:
  - cmd: cat {shared.generated.txt}
```

`setup` and `teardown` each accept either a single entry or a sequence of entries. An entry is
either a bare command string, or a mapping with `cmd` plus optional `env`, `stdin_file`, and
`timeout`:

```yaml
setup:
  - cmd: seed the database
    env:
      DB_URL: "{shared.db.sock}"
    stdin_file: fixtures/seed.sql   # relative to this .dats file; raw, unexpanded content
    timeout: 10s                    # must be > 0; defaults to 30s
  - echo plain strings still work
```

`env` values expand `{shared.X}` only, same as `cmd`, and are added on top of the inherited
environment (and any run-wide entries a library caller set via `Options.Env`) — scoped to that
one command, not inherited by later hooks or tests. `stdin_file` names a host file piped to the
command's stdin verbatim (unexpanded); a
relative path resolves against the directory holding this `.dats` file, like `inputs.copy`.
`timeout` bounds the command and must be greater than 0 — unlike a test's `timeout` (0/omitted
= unbounded), a hook command always has a bound, defaulting to 30s when unstated.

Blank commands, non-string/non-mapping entries, an entry missing `cmd`, an unknown mapping key,
and empty lists are all parse errors (`setup: must list at least one command`). `shared` must
declare at least one fixture across
`files` and `copy` combined (`shared: must declare at least one file under files or copy`) —
see [Copy Fixtures](#copy-fixtures-inputscopy-and-sharedcopy) for `copy`. File names in either
map follow the same locality rule as `inputs.files` names — relative paths that stay inside
the shared directory, nested names like `sub/file.txt` allowed — and a name may appear in
only one of the two maps.

### `{shared.X}` Placeholders

`{shared.<filename>}` expands to a path under the file's `shared/` directory, e.g.
`/tmp/dats-xxxxxx/shared/<filename>`. Like `{outputs.X}`, the name does not need to be
declared — any **local relative** name resolves, and a non-local name (one containing `..` or
an absolute path) is left verbatim, so a placeholder can never address a path outside the
shared directory.

`{shared.X}` expands everywhere `{inputs.X}`/`{outputs.X}` already expand: the command,
`inputs.files` contents, and `inputs.env` values (`inputs.stdin` stays unexpanded, as
always). It additionally expands — as the **only** namespace — in setup commands, teardown
commands, and `shared.files` contents; `{inputs.X}` and `{outputs.X}` are per-test
namespaces and pass through verbatim there.

### Execution Order and Failure Semantics

Per file, the runner:

1. Creates the `shared/` directory (alongside the per-test directories; preserved by
   `--keep-temp`).
2. Writes the `shared.files` fixtures into it.
3. Runs the `setup` commands in declared order — through the same `bash -c` path as test
   commands, in the working directory of the `dats` invocation, with the inherited
   environment (plus `GOCOVERDIR` under `--coverdir`, exactly like test commands, plus the
   entry's own `env`), the entry's `stdin_file` content on stdin (or none), and bounded by
   the entry's `timeout` (30s when unstated), capturing stdout and stderr. On timeout, the
   command's process group is killed and the entry fails with `command timed out after
   <duration>`. Teardown commands run the same way.
4. Runs the tests.
5. Always runs **all** `teardown` commands in declared order — after the tests, after test
   failures, and even when setup failed. One failing teardown command does not stop the
   rest. (Teardown does not apply to files that fail to parse: nothing ran.)

If a setup command fails (non-zero exit or signal death), the remaining setup commands are
skipped, the file's tests do **not** run, and every test is reported as a normal failure
with reason `file setup failed` — loudly, never as "skipped" — after a file-level diagnostic
naming the failing command, its exit status, and its captured output. The run exits 1.

If a teardown command fails, a file-level diagnostic is printed and the **file** is marked
failed — the run exits 1 even when every test passed — with the summary line annotated
`teardown failed`. Individual test lines stay as they ran.

### Concurrency Contract

Setup and teardown are per-file barriers: a future parallel mode (`-j`) may run tests
concurrently within and across files, but no test in a file starts before that file's setup
completes, and teardown starts only after the file's last test finishes. Tests should treat
the shared directory as **read-only**; tests that mutate shared state are undefined under
parallelism. Setup/teardown of different files may overlap in parallel mode — do not assume
exclusive access to global resources.

## Sandbox

Test commands are sandboxed by default (`--sandbox=auto`: the platform's native sandbox —
bubblewrap on Linux, `sandbox-exec` on macOS — falling back to docker; see
[cli.md](cli.md#sandboxing---sandbox) for what each backend isolates). The
optional file-level `sandbox` key narrows that for one file, and is the declarative way to
opt out:

```yaml
sandbox: false        # this file's commands need the host; run them there
```

```yaml
sandbox:
  enabled: true       # default; `false` is the same as `sandbox: false`
  network: false      # default true; false runs commands with no network
  image: alpine:3.20  # docker backend only; overrides --sandbox-image (must ship bash).
                      # Ignored by bwrap and seatbelt, which use the host's own filesystem
```

There is no key for extra writable host paths, and that is deliberate. Somewhere to write is
the file's temp directory, which every backend gives you; a command that genuinely needs the
host is not a sandboxed command and says `sandbox: false`. A per-path hole is neither, and
its consequences are invisible to the person reading the file. To bring an *existing* host
file into that writable temp directory — not a new path on the host, a copy inside the
sandbox's own writable area — use `inputs.copy`/`shared.copy`; see
[Copy Fixtures](#copy-fixtures-inputscopy-and-sharedcopy).

The block covers **every** command in the file — its tests and its `setup`/`teardown` hooks
alike. It is file-level, not per-test: one file's commands share one temp directory, one
shared directory, and one hook lifecycle, so a per-test sandbox would make those shared paths
mean different things to different tests.

The CLI's choice is the outer bound. A file can narrow it (opt out, cut the network) or
adjust it (image), never widen it: under `--no-sandbox` the whole block
is inert, and `sandbox: true` does not force a sandbox onto a run that opted out.

Both shapes are validated strictly: unknown or duplicate keys, a non-boolean `enabled` or
`network`, an empty `image`, and an empty mapping are all parse
errors — a misspelled key must never silently disable isolation. `sandbox:` with no value at
all is the same as omitting it. `{matrix.X}` is rejected in `image`: the
sandbox is resolved once per file, before any matrix instance exists.

### What a sandboxed file can rely on

- Fixtures, `{inputs.X}`, `{outputs.X}`, `{shared.X}`, `inputs.env`, `inputs.stdin`, output
  files and snapshots all work unchanged — the file's temp directory is writable inside the
  sandbox.
- Writes anywhere else fail (bwrap, seatbelt) or vanish with the container (docker). A test
  with something to write puts it in the temp directory (`{outputs.X}`, `{shared.X}`, or its
  own `mktemp -d` under the private /tmp).
- Under bwrap and docker a host path outside the working directory is not READABLE either:
  those backends expose the same confined set. A file that has to reach the rest of the
  machine runs unsandboxed (`sandbox: false`) — a decision the file states in one line,
  rather than a list of paths that quietly adds up to the same thing.
- Under the docker backend the command runs inside the image, so the tools available are the
  image's, not the host's, and only `inputs.env` values and `GOCOVERDIR` are carried in.

## Copy Fixtures: `inputs.copy` and `shared.copy`

`inputs.files`/`shared.files` author a fixture's content inline as YAML text. `copy` is the
other way to get a file into the writable temp directory: instead of content, it names an
**existing host file** to copy in.

```yaml
shared:
  copy:
    fixture.bin: ../fixtures/fixture.bin   # once per file, into shared/

tests:
  - desc: modifies a copied-in fixture
    inputs:
      copy:
        config.json: fixtures/config.json  # once per test, into the test's inputs/
    cmd: echo patched >> {inputs.config.json}; cat {inputs.config.json}
```

Each is a map of **destination filename** (addressed the same way as a `files` entry —
`{inputs.X}`/`{shared.X}`) to a **host source path**. A relative source resolves against the
directory holding the `.dats` file being run, so a copy source is portable regardless of
dats' own working directory; an absolute source is used as-is. The copy runs on the host,
before the sandbox starts — the destination lands in the same writable temp directory as
every other fixture, so a sandboxed command can modify its own copy freely without touching
the original.

This is the read-write counterpart of the sandbox's read-only bind mount of the working
directory (see [Sandbox](#sandbox) above): reach for `copy` when a test needs to *mutate* a
real fixture, or when the fixture is too large or too binary to spell out as YAML text under
`files`. The copy preserves the source's permission bits, so a script pulled in this way keeps
its executable bit.

A destination name follows the same locality rule as a `files` name (relative, no `..`, no
absolute paths — rejected at parse time) and may not also appear under `files` in the same
block (`"X" is declared under both files and copy`); an empty source path is also a parse
error. `inputs.copy` is inside the matrix substitution scope — `{matrix.X}` substitutes into
the **source** path first, so a copy source can vary per instance:

```yaml
tests:
  - cmd: cat {inputs.variant.bin}
    matrix:
      variant: [a, b]
    inputs:
      copy:
        variant.bin: fixtures/{matrix.variant}.bin
```

`shared.copy` runs at file scope, before any matrix instance exists, so `{matrix.X}` there is
rejected the same way it is in `shared.files` (`shared copy "X": {matrix.x} is not available
outside tests`).

### Why not a heredoc or herestring?

A shell heredoc (`<<WORD`, `<<-WORD`, `<<~WORD`) or herestring (`<<<`) in `cmd`, `setup`, or
`teardown` is rejected at parse time, each with its own error naming why:

```
test 1: cmd: must not use a shell heredoc (<<WORD) -- write the file and pull it in with inputs.files/inputs.copy or shared.files/shared.copy instead
```

```
test 1: cmd: must not use a shell herestring (<<<) -- use inputs.stdin (or a pipe within cmd) instead of redirecting from the end of the line
```

A heredoc embeds a file's content inline in a single shell string — exactly what `files` and
`copy` exist to do declaratively, with names dats can address via `{inputs.X}`/`{shared.X}`,
validate for locality, and normalize in snapshot goldens. A heredoc bypasses all of that: it
is invisible to every mechanism above. If you want a file, write the file and copy or bind
mount it in.

A herestring is a different construct — `cmd <<< "text"` feeds `text` to `cmd`'s stdin on one
line, no multi-line delimiter involved — but it is banned for a related reason: it puts the
data source at the *end* of the line, working against bash's normal left-to-right flow
(`producer | consumer`), and duplicates what `inputs.stdin` (declarative, placeholder-expanded,
readable in the test's own block) or an ordinary pipe already does inside `cmd`. Use one of
those instead: `inputs: {stdin: "text"}` with `cmd: cat`, or `cmd: echo text | cat`.

## Test Object

Each test has these fields:

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `cmd` | string | Yes | - | Command to execute |
| `desc` | string | No | Value of `cmd` | Test description/name |
| `exit` | int or string | No | `0` | Expected exit code |
| `timeout` | int or string | No | none | Per-test timeout (seconds or duration string) |
| `matrix` | object | No | - | Parameter variables expanding the test into one instance per combination — see [Matrix (Parameterized) Tests](#matrix-parameterized-tests) |
| `inputs` | object | No | - | Stdin, input files, and environment variables |
| `outputs` | object | No | - | Output validations |

### Minimal Test

```yaml
tests:
  - cmd: echo hello
```

### Full Test

```yaml
tests:
  - desc: comprehensive example
    exit: 0
    timeout: 5s
    cmd: process {inputs.data.txt} -o {outputs.result.txt}
    inputs:
      stdin: "optional stdin content"
      files:
        data.txt: "input file content"
      env:
        PROCESS_MODE: strict
    outputs:
      stdout:
        - "pattern to match"
      stderr:
        - "expected stderr"
      "!stdout":
        - "must not appear"
      files:
        result.txt:
          exists: true
          match:
            - "expected content"
```

---

## Matrix (Parameterized) Tests

A test may declare `matrix:` — a mapping of variable name to a list of scalar values —
expanding the test into one instance per **combination** of values (cartesian product):

```yaml
tests:
  - desc: greets
    cmd: echo "{matrix.greeting}, {matrix.name}!"
    matrix:
      greeting: [hello, howdy]
      name: [alice, bob]
    outputs:
      stdout:
        - "{matrix.greeting}, {matrix.name}!"
```

This runs as 4 tests. **Every instance always runs** — dats has no test filtering or
selection by design; the expanded list IS the plan, and a matrix test cannot run "some"
of its combinations.

### Expansion Order and Instance Names

Expansion is deterministic: variables keep their **declaration order**, and the **last**
declared variable varies **fastest**. The example above produces, in order:

```
ok 1 - greets [greeting=hello, name=alice]
ok 2 - greets [greeting=hello, name=bob]
ok 3 - greets [greeting=howdy, name=alice]
ok 4 - greets [greeting=howdy, name=bob]
```

Each instance's reported name is the test's `desc` (or its `cmd`, after substitution,
when no desc is set) followed by the label ` [k=v, k2=v2]` — assignments in declaration
order. The label appears on every reported line, including single-value matrices
(`[k=v]`) and the `file setup failed` lines when file-level setup fails, so a failing
instance is always identifiable. Instances count as ordinary tests everywhere: the
file header's `(N tests)`, the summary counts, and the exit code all see the expanded
list, and each instance gets its own private `test-<index>/` fixture directory
(identical fixture names across instances never collide).

### Variables and Values

Variable names must match `^[A-Za-z_][A-Za-z0-9_]*$`. Each variable lists at least one
**scalar** value — string, number, or boolean. A value is substituted as its literal
YAML text: `1.50` stays `1.50` (never reformatted), `true` stays `true`, and a quoted
string contributes its content. Because values are compared after this stringification,
`[1.50, "1.50"]` is a duplicate — and duplicates, which could only produce
byte-identical instances, are parse errors.

### Substitution Scope

`{matrix.X}` substitutes into exactly these strings of the declaring test:

- `desc`
- `cmd`
- `inputs.stdin`
- `inputs.files` **contents** (values)
- `inputs.copy` **sources** (values) — so a copy source can vary per matrix instance
- `inputs.env` **values**
- every output pattern string: `stdout`, `stderr`, `!stdout`, `!stderr` in both the
  list and line-map forms, and `files`/`!files` `match`/`notMatch` entries
- every **string** scalar inside `json_output` — keys and values (non-string scalars
  like numbers and booleans are untouched, so substitution cannot change a value's
  JSON type)

NOT substituted (always literal): fixture file **names** (the keys under
`inputs.files`, `inputs.copy`, `outputs.files`, `outputs.!files`), env var **names**,
`exit`, `timeout`, and the `matrix` block itself. A file name containing `{matrix.x}` is
not a reference — it is a (strange) literal file name.

### Layering with `{inputs.X}`/`{outputs.X}`/`{shared.X}`

Matrix substitution is a separate, earlier layer: it happens once per instance at
expansion time, before any runtime placeholder expansion. That is why `inputs.stdin`
gets matrix substitution even though it never gets runtime expansion. A matrix value
may itself contain `{inputs.X}`/`{outputs.X}`/`{shared.X}` — after substitution the
text behaves like any other text in that position, expanding where those namespaces
normally expand:

```yaml
tests:
  - desc: reads the file the matrix names
    cmd: cat {matrix.path}
    matrix:
      path: ["{shared.config.json}"]
```

Substitution is a **single pass**: substituted text is never re-scanned, so a matrix
value containing a literal `{matrix.y}` stays literal — it is not expanded again.

### Strict Errors

All of these are parse errors (so `dats syntax` catches them without running anything):

| Problem | Error |
|---------|-------|
| `matrix:` present but empty (`{}`) | `matrix must declare at least one variable` |
| `matrix:` not a mapping | `matrix must be a mapping of variable names to value lists` |
| Bad variable name | `matrix variable name "bad-name" must match ^[A-Za-z_][A-Za-z0-9_]*$` |
| Variable declared twice | `matrix variable "x" declared more than once` |
| Value not a sequence | `matrix variable "x" must list its values as a sequence` |
| Empty value list | `matrix variable "x" must list at least one value` |
| Non-scalar value (mapping/sequence/null) | `matrix variable "x" value 2: values must be scalar strings, numbers, or booleans` |
| Duplicate value (post-stringification) | `matrix variable "x" lists duplicate value "1.50"` |
| Reference to an undeclared variable | `test 1: {matrix.nope} is not a declared matrix variable (declared: greeting, name)` |
| `{matrix.X}` in a test with no matrix | `test 1: {matrix.x} is used but the test declares no matrix` |
| Empty reference `{matrix.}` | `test 1: {matrix.} must name a matrix variable` |
| `{matrix.X}` in a setup/teardown command | `setup command 1: {matrix.x} is not available outside tests` |
| `{matrix.X}` in `shared.files` contents | `shared file "config.json": {matrix.x} is not available outside tests` |
| `{matrix.X}` in `shared.copy` sources | `shared copy "fixture.bin": {matrix.x} is not available outside tests` |

The reference check scans exactly the substitution scope above: every `{matrix.X}`
there must name a variable declared by **that** test's matrix. Setup and teardown
commands and shared file contents run once per file, where no matrix instance exists,
so matrix placeholders are rejected there even when some test declares the variable.
`matrix:` with an explicit null value is treated as an absent key (like `shared:`).

---

## Backwards Compatibility

Files that use none of the new keys (`shared`, `setup`, `teardown`, `matrix`) parse and
behave identically, with one caveat: literal text shaped like the two new placeholder
namespaces changes meaning.

- `{shared.<name>}` with a local relative `<name>` previously passed through as literal
  text; it now expands to a path under the file's `shared/` directory wherever
  placeholders expand (`cmd`, `inputs.files` contents, `inputs.env` values).
- `{matrix.<name>}` anywhere in the matrix substitution scope is now validated: in a
  test without a `matrix` block it is a parse error (`test N: {matrix.<name>} is used
  but the test declares no matrix`). Such a file previously ran with the text kept
  literal.

Text that only *resembles* a placeholder without being one — a non-local name like
`{shared.../x}`, an empty reference in a namespace that never validates (`{shared.}`),
or any other brace construct — still passes through verbatim.

A `cmd`, `setup`, or `teardown` string containing a shell heredoc (`<<WORD`) or herestring
(`<<<`) now fails to parse (see
[Copy Fixtures](#copy-fixtures-inputscopy-and-sharedcopy)) — the one genuinely
backwards-incompatible change here, since such a file previously ran either as ordinary
shell. Rewrite a heredoc with `inputs.files`/`inputs.copy` or `shared.files`/`shared.copy`,
and a herestring with `inputs.stdin` or a pipe.

Sandboxing changes runtime behavior rather than parsing, and it applies to files that
declare nothing: commands that used to write anywhere on the host now write only inside
their temp directory, and a machine with neither backend installed fails the run instead of
executing it. Existing files keep passing as long as their commands stay inside `{outputs.X}`
/`{shared.X}` (the whole point of those placeholders). A file that legitimately needs the
host declares `sandbox: false`; a whole run opts out with `--no-sandbox`.

---

## Command Field (`cmd`)

The command is run with `bash -c` in the working directory of the `dats` invocation (the
runner does not change directory). Fixture files live in a fresh per-run temp directory and
are addressed by absolute path through placeholders. A shell heredoc (`<<WORD`) or herestring
(`<<<`) in `cmd` is rejected at parse time — see
[Copy Fixtures](#copy-fixtures-inputscopy-and-sharedcopy).

### Input Placeholders

`{inputs.<filename>}` expands to the absolute path of an input fixture file, e.g.
`/tmp/dats-xxxxxx/test-<index>/inputs/<filename>`.

```yaml
inputs:
  files:
    data.txt: "content"
cmd: cat {inputs.data.txt}
```

### Output Placeholders

`{outputs.<filename>}` expands to a path under the test's `outputs/` directory where the
command should write output, e.g. `/tmp/dats-xxxxxx/test-<index>/outputs/<filename>`. The path
is provided; the command is responsible for creating the file. The name does not need to
appear under `outputs.files` — any **local relative** name resolves. A non-local name (one
containing `..` or an absolute path, e.g. `{outputs.../escape}`) is left verbatim, so a
placeholder can never address a path outside the test directory.

Names that DO appear under `files`/`!files` get their parent directories created before the
command runs, so a command can write a declared nested output like
`{outputs.sub/report.txt}` directly. For undeclared nested names, the command must create the
intermediate directories itself.

```yaml
cmd: process -o {outputs.result.bin}
outputs:
  files:
    result.bin:
      exists: true
```

### Shared Placeholders

`{shared.<filename>}` expands to a path under the file-wide `shared/` directory — see
[File-Level Setup, Teardown, and Shared Fixtures](#file-level-setup-teardown-and-shared-fixtures).

### Multiple Placeholders

```yaml
cmd: diff {inputs.a.txt} {inputs.b.txt} > {outputs.diff.txt}
```

### Placeholders in Input File Contents

The same expansion is applied to the contents of `inputs.files`, so a fixture (e.g. a script
or program the command runs) can reference other input paths and output paths:

```yaml
inputs:
  files:
    script.sh: 'cp {inputs.data.txt} {outputs.copy.txt}'
    data.txt: "content"
cmd: bash {inputs.script.sh}
outputs:
  files:
    copy.txt:
      match:
        - "content"
```

`{inputs.<name>}` for a name not declared under `inputs.files` is left untouched (in both the
command and file contents), as is any other brace construct.

---

## Exit Code Field (`exit`)

### Integer Values (0-255)

Bare and quoted integers are equivalent; the 0-255 range is enforced either way:

```yaml
exit: 0      # success
exit: 1      # generic failure
exit: 127    # command not found
exit: "3"    # quoted integer, same as 3
```

Floats are rejected at parse time (`exit: 1.5` is an error, as is an integral float like
`2.0`) — an exit code is always an integer.

### Variable Names

Exactly two names are recognized (any other `EXIT_*` name is rejected at parse time, since the
runner could never resolve it):

- `EXIT_SUCCESS` = 0
- `EXIT_FAILURE` = 1

```yaml
exit: EXIT_SUCCESS
exit: EXIT_FAILURE
```

### Signal Deaths

A command terminated by a signal has no normal exit code; it surfaces as `-1` with the signal
named in the failure message:

```
# expected exit code 0, got -1 (killed by signal: killed)
```

---

## Timeout Field (`timeout`)

Bounds how long the command may run. Accepts an integer number of seconds (bare or quoted — a
quoted bare integer like `"5"` also means seconds) or a Go duration string. `0` or an omitted
field means no timeout.

```yaml
timeout: 5       # 5 seconds
timeout: "5"     # quoted integer, also 5 seconds
timeout: 500ms   # 500 milliseconds
timeout: 1m30s   # 90 seconds
```

Floats are rejected at parse time — `timeout: 0.9` is an error, not 0 seconds; write
fractional seconds as a duration string like `900ms` or `1.5s`.

When the deadline elapses, the command's **whole process group** is killed — background
children included, not just the direct `bash` process. The test then fails with only a
`command timed out after <duration>` message; every other assertion is skipped, since checking
partial output or missing files would bury the real cause under secondary failures. The
stdout/stderr captured before the kill are still shown in verbose mode.

A command that leaves orphaned background processes holding its stdout/stderr open cannot
block the runner indefinitely: the pipes are force-closed about 1 second after the main
process exits (timeout or not). Output written by such stragglers after that point is not
captured.

---

## Inputs Block

```yaml
inputs:
  stdin: "content piped to stdin"
  files:
    filename.txt: "file content"
    another.txt: |
      multi-line
      content
  copy:
    real.bin: fixtures/real.bin   # copies an existing host file in, writable
  env:
    MY_VAR: "value"
```

See [Copy Fixtures](#copy-fixtures-inputscopy-and-sharedcopy) for `copy`.

### `stdin`

Content piped to the command's standard input.

```yaml
inputs:
  stdin: "hello world"
cmd: grep hello
```

### `files`

Map of filename to content. Each file is created before the test runs; reference it in the
command with `{inputs.<filename>}`. Nested paths (e.g. `sub/dir/file.txt`) are supported.
Contents go through `{inputs.X}`/`{outputs.X}` placeholder expansion — see
[Placeholders in Input File Contents](#placeholders-in-input-file-contents).

File names must be relative paths that stay inside the test directory — `..` traversal and
absolute paths are rejected at parse time (so `dats syntax` catches them) and again at
fixture-setup time. The same rule applies to the names under `outputs.files` and
`outputs.!files`.

```yaml
inputs:
  files:
    config.json: |
      {"key": "value"}
    data.csv: "a,b,c"
```

### `env`

Map of environment variable name to value. The variables are **added** to the environment the
command inherits from `dats` (they do not replace it), appended in sorted key order so runs
are deterministic. Values go through the same `{inputs.X}`/`{outputs.X}` placeholder expansion
as the command, so a variable can carry a fixture's absolute path:

```yaml
inputs:
  files:
    cfg.json: '{"mode": "test"}'
  env:
    MY_VAR: hello
    CONFIG_PATH: "{inputs.cfg.json}"
cmd: 'echo "$MY_VAR"; cat "$CONFIG_PATH"'
```

With `--coverdir`, `GOCOVERDIR` is appended after the test's variables, so the flag wins even
over a test's own `GOCOVERDIR` entry.

---

## Outputs Block

```yaml
outputs:
  stdout:        # patterns that MUST appear (or line-number map)
  stderr:        # patterns that MUST appear (or line-number map)
  "!stdout":     # patterns that must NOT appear (or line-number map)
  "!stderr":     # patterns that must NOT appear (or line-number map)
  files:         # output file checks
  "!files":      # negated output file checks
  snapshot:      # golden-file (snapshot) assertion on stdout/stderr
  json_output:   # expected JSON value of the whole stdout
```

### Pattern Lists

A list of **literal substrings**, each of which must appear somewhere in the output. They are
not regexes — metacharacters have no special meaning:

```yaml
outputs:
  stdout:
    - "expected text"
    - "another pattern"
```

### Line-Specific Checks

Use integer keys (0-indexed) with Go/RE2 regex values. Each regex is searched **unanchored**
within its line — use `^...$` to pin the whole line. Addressing a line the output does not
have fails the test (empty output has zero lines):

```yaml
outputs:
  stdout:
    0: "^first line$"
    2: "^third line$"
    5: "pattern on line 6"
```

Line keys must be unique, non-negative integers: a duplicate line number (including different
spellings such as `0` and `00`) and a negative line number are both parse errors.

**Note**: You cannot mix pattern lists and line-specific checks in the same block. Use one
format or the other.

### Negated Output Checks

`!stdout` and `!stderr` assert substring patterns do NOT appear:

```yaml
outputs:
  "!stdout":
    - "error"
    - "failed"
  "!stderr":
    - "warning"
```

Like the positive checks, negated checks also accept the line-specific map form. Each regex
must NOT match (unanchored search) within the given 0-indexed line. A line that does not
exist passes — there is nothing there to match:

```yaml
outputs:
  "!stdout":
    0: "error"        # line 0 must not contain "error"
    2: "^warning"     # line 2 must not start with "warning" (also passes if there is no line 2)
```

---

## File Checks

Validate output files created by the command:

```yaml
outputs:
  files:
    output.bin:
      exists: true
      match:
        - "expected pattern"
        - "another pattern"
      notMatch:
        - "should not contain"
```

File names must be relative paths that stay inside the test directory (`..` and absolute
paths are rejected at parse time). Nested names like `sub/report.txt` are allowed; parent
directories of every file declared under `files`/`!files` are created before the command
runs. When several file checks fail, the failures are reported in sorted-by-name order.

### Fields

| Field | Type | Description |
|-------|------|-------------|
| `exists` | boolean | Whether the file should exist |
| `match` | string[] | Regex patterns that must match the file's contents |
| `notMatch` | string[] | Regex patterns that must NOT match the file's contents |

### Empty Checks Are Implicit Existence Assertions

A check with none of the three fields — written `{}` or left null — asserts existence rather
than passing vacuously. Under `files` the file must exist; under `!files` it must NOT exist:

```yaml
outputs:
  files:
    out.txt: {}       # out.txt must exist
    log.txt:          # null value, same meaning: log.txt must exist
  "!files":
    stray.txt: {}     # stray.txt must NOT exist
```

### Negated File Checks (`!files`)

`!files` accepts the same `exists`/`match`/`notMatch` fields as `files`, but asserts the
NEGATION of each check:

| Field | `files` meaning | `!files` meaning |
|-------|-----------------|------------------|
| `exists: true` | file must exist | file must NOT exist |
| `exists: false` | file must NOT exist | file must exist |
| `match: [p]` | contents must match `p` | contents must NOT match `p` (a missing file passes) |
| `notMatch: [p]` | contents must NOT match `p` (a missing file passes) | contents must match `p` (a missing file fails) |

The common use is asserting that a file must NOT exist or must NOT contain something:

```yaml
outputs:
  "!files":
    error.log:
      exists: true        # error.log must NOT exist
    report.txt:
      match:
        - "FAILED"        # report.txt must NOT contain FAILED
```

---

## JSON Output (`json_output`)

`json_output` asserts that the whole stdout is a single JSON value that deep-equals the given
value:

```yaml
tests:
  - desc: emits the expected JSON document
    cmd: mytool --json
    outputs:
      json_output:
        name: dats
        count: 2
        tags: [a, b]
```

Comparison rules:

- Object keys are **order-insensitive**; arrays are **order-sensitive**.
- Numbers compare by value (`2` equals `2.0`).
- The expected value may be any JSON value — object, array, string, number, bool, or `null`
  (`json_output: null` asserts stdout is exactly the JSON `null`).
- Stdout must contain exactly one JSON value (trailing whitespace is fine, trailing data is
  not). Non-JSON stdout fails the assertion.

On mismatch the failure lists each difference with its JSONPath-style location:

```
# json_output: at $.tokens[3].kind: expected "Ident", got "Keyword"
# json_output: at $: missing key "eof" (expected true)
```

---

## Snapshot Assertions (outputs.snapshot)

`snapshot` asserts that captured output byte-matches a stored **golden file**, so a test can
pin an entire output verbatim without spelling it out in YAML. Two forms:

```yaml
tests:
  - desc: renders the report
    cmd: mytool report
    outputs:
      snapshot: true          # snapshot stdout (boolean shorthand)

  - desc: split streams
    cmd: mytool report --warnings
    outputs:
      snapshot:               # per-stream form: enable stdout and/or stderr
        stdout: true
        stderr: true
```

`snapshot: false` is the documented toggle-off — identical to omitting the key, handy for
temporarily disabling a snapshot without deleting the block. The per-stream mapping must
enable at least one stream; a mapping that enables neither (empty, or explicit falses),
an unknown key, a non-boolean value, and a duplicate key are all parse errors.

### Golden Storage and Naming

Goldens live in a `.snapshots` directory next to the `.dats` file — `examples/demo.dats`
keeps its goldens in `examples/demo.snapshots/`. Each enabled stream of each test instance
gets its own file:

```
<file>.snapshots/NNN-<slug>.<stream>.golden
```

- `NNN` is the canonical **1-based instance number** — the same number the CLI prints in
  `ok N -` and the JSON report's `index` — zero-padded to three digits.
- `<slug>` derives from the instance's display name (desc — or the command when there is no
  desc — plus the matrix label): lowercased, runs of characters outside `[a-z0-9]` become
  single dashes, trimmed, truncated to 60 characters, with `test` as the fallback for a name
  that slugs to nothing.
- `<stream>` is `stdout` or `stderr`.

Names derive from position and name, so **renaming, reordering, or removing tests changes
the expected golden names** — re-bless with `--update`, which writes the new files and
prunes the now-stale ones.

A **matrix** test snapshots per instance: every combination gets its own golden (the matrix
label is part of the slug), e.g. `003-greets-who-alice.stdout.golden` and
`004-greets-who-bob.stdout.golden`.

### Comparison and Normalization

Comparison is **byte-exact after normalization**: before comparing (or writing), the
framework's temp paths in the output are replaced with stable tokens, longest path first —

| Text in output | Token |
|----------------|-------|
| the instance's test directory (`/tmp/dats-xxxxxx/test-N`) | `{testdir}` |
| the file's shared directory (`/tmp/dats-xxxxxx/shared`) | `{shareddir}` |
| the per-run temp root (`/tmp/dats-xxxxxx`) | `{tmproot}` |

— so a command that prints an `{inputs.X}` path produces a golden containing
`{testdir}/inputs/X`, reproducible across runs and machines. Everything else is compared
byte for byte: **trailing newlines are significant** (the common echo-vs-printf slip is
called out specially as `output differs only by a trailing newline`). Known limitation:
output that *literally* contains `{testdir}`, `{shareddir}`, or `{tmproot}` is
indistinguishable from a normalized path in the golden.

Without `--update`, a missing golden is a failure:

```
# snapshot: stdout: golden file demo.snapshots/001-renders.stdout.golden does not exist (run with --update to create it)
```

and a mismatch names the first difference (0-indexed lines, matching the line-check
convention):

```
# snapshot: stdout: output does not match golden file demo.snapshots/001-renders.stdout.golden (line 2: expected "total 4", got "total 5")
```

Like every other assertion, snapshots are skipped when the command did not run to
completion — a timeout reports only `command timed out after X`.

### Updating Goldens (--update)

`dats --update <files>` rewrites goldens from actual output instead of failing:

- A golden is written only when it is **missing or differs**; an up-to-date golden is not
  rewritten (no mtime churn) and not listed.
- Goldens **never update from a failing instance**: if the instance has any other failure
  (wrong exit code, a failed pattern check, ...), its goldens are neither written nor
  compared — fix the test first.
- Stale `*.golden` files in the file's snapshot directory — instances or streams that no
  longer exist — are **pruned** (after a clean file setup only), and the directory itself is
  removed when pruning empties it. Non-`.golden` files are never touched.
- Every write and prune is listed on stdout (`# updated golden: ...`,
  `# pruned stale golden: ...`), with an end-of-run summary line
  (`Updated 2 golden file(s), pruned 1 stale`).

See [CLI Usage](cli.md#updating-snapshots---update) for a worked example.

### Scope Notes

- Only the two output **streams** can be snapshotted. A `files` variant is deliberately
  rejected for v1: `outputs.files` `match`/`notMatch` already asserts file contents, and a
  files variant would multiply the naming scheme (per instance × per declared file) —
  revisit if demanded.
- Snapshots compose with `-j` — golden files are per-instance unique, results are
  deterministic, and output stays byte-identical to a serial run — and appear in
  `--report-junit`/`--report-json` as ordinary assertion failures (no report format
  change).

---

## Complete Field Reference

```yaml
shared:                    # optional file-level fixtures
  files:
    <name>: string         # filename: content ({shared.X} placeholders expanded)
  copy:
    <name>: string         # filename: host source path (relative to the .dats file)
setup: hookCommand|[]      # optional; command(s) run once before the tests
teardown: hookCommand|[]   # optional; command(s) always run once after the tests
# hookCommand = string, or:
#   cmd: string             # required
#   env: {name: value}      # optional; {shared.X} only
#   stdin_file: string      # optional; host path, relative to the .dats file
#   timeout: int|string     # optional; must be > 0, defaults to 30s
tests:
  - desc: string           # optional, defaults to cmd value
    exit: int|string       # optional, defaults to 0
    timeout: int|string    # optional, seconds or duration string; 0/omitted = no timeout
    cmd: string            # required; a shell heredoc (<<WORD) or herestring (<<<) is rejected at parse time
    matrix:                # optional; expands the test into one instance per combination
      <name>: [scalar, ...]  # variable: at least one scalar value, referenced as {matrix.<name>}
    inputs:
      stdin: string        # optional
      files:               # optional
        <name>: string     # filename: content
      copy:                # optional
        <name>: string     # filename: host source path (relative to the .dats file)
      env:                 # optional
        <name>: string     # env var: value (placeholders expanded)
    outputs:
      stdout: []|{}        # pattern list or line checks
      stderr: []|{}        # pattern list or line checks
      "!stdout": []|{}     # negated patterns
      "!stderr": []|{}     # negated patterns
      files:
        <name>:            # empty check ({}/null) = must exist
          exists: bool
          match: []
          notMatch: []
      "!files":
        <name>:            # empty check ({}/null) = must NOT exist
          exists: bool
          match: []
          notMatch: []
      snapshot: bool|{}    # golden-file assertion: true = stdout, or {stdout: bool, stderr: bool}
      json_output: any     # expected JSON value of the whole stdout
```
