# Machine-Readable Reports

`dats [test]` can write the results of a run to report files for CI systems and editor integrations:

```bash
dats --report-junit report.xml tests/     # JUnit XML
dats --report-json report.json tests/     # JSON
dats --report-junit r.xml --report-json r.json tests/   # both at once
```

Both flags are long-only and require a value (the output path). Parent directories of the path are created automatically.

## When report files are (and are not) written

- Reports are written at the **end of the run whenever the run executed** — and especially when it failed: failing tests, setup failures, teardown failures. Default stdout output and exit-code semantics are unchanged.
- Reports are written from the **same result data in serial and parallel (`-j`) runs**, in the same canonical order: files in the order given. A failed file-level setup appears before the file's instances. Failed teardown commands appear after them.
- **Hard errors that abort the run** keep their existing control flow and write **no** report file: an unreadable or unparseable `.dats` file, invalid arguments. In jobs mode a parse error in any file aborts before anything runs — also without reports.
- `dats syntax` never writes reports (the flags parse but are ignored).
- A report file that **cannot be written** is a real error: the message is printed to stderr and the process exits 1, even when every.

## Counts contract

The two formats count differently, by design:

- The **JSON** `summary.tests`/`passed`/`failed` count **test instances only** and always equal the CLI summary numbers (`N/M passed, K failed`). File-level setup/teardown failures are reported in `setup_failure`/`teardown_failures`, never as entries in `tests`.
- The **JUnit** `tests`/`failures` attributes additionally count the synthetic `[setup]`/`[teardown]` cases (most JUnit consumers can only surface what is a testcase), so JUnit totals are **≥ the CLI summary counts** whenever a hook failed.

## JSON format

Top-level document (`format_version` 1):

| Field | Meaning |
|-------|---------|
| `format_version` | Integer format version, currently `1` (see [Stability](#stability)) |
| `ok` | Whether the run passed as a whole — mirrors the process exit code (`true` ↔ exit 0): every instance passed, no setup or teardown failure in any file |
| `summary.files` | Number of `.dats` files run |
| `summary.tests` / `summary.passed` / `summary.failed` | Instance counts — exactly the CLI summary numbers |
| `summary.wall_seconds` | Wall time of the whole execution phase, in seconds |
| `files` | One entry per file, in canonical order |

Each entry of `files`:

| Field | Meaning |
|-------|---------|
| `path` | The file path as given on the command line (or as discovered) |
| `ok` | Whether the file passed as a whole (tests, setup, and teardown) |
| `duration_seconds` | Sum of the file's instance durations (under `-j` this can exceed `wall_seconds`) |
| `setup_failure` | `null`, or `{"command", "detail", "stdout", "stderr"}` for the setup step that failed |
| `teardown_failures` | Array (possibly empty, never `null`) of the same shape, in declared order |
| `tests` | One entry per test instance, in expansion order |

Each entry of `tests`:

| Field | Meaning |
|-------|---------|
| `name` | The expanded instance name — desc (or command) plus the matrix label, e.g. `greets [who=alice]`; the same name the CLI prints |
| `index` | The canonical 1-based instance number within the file — matches the CLI's `ok N -` numbering |
| `ok` | Whether the instance passed |
| `duration_seconds` | Instance duration in seconds (0 for instances that never ran, e.g. after a setup failure) |
| `failures` | Assertion failure messages (empty array when `ok` is `true`) |
| `command` | The command as executed, after placeholder expansion (`""` when the instance never ran) |
| `stdout`, `stderr` | Captured output — present exactly when `ok` is `false` (even if empty), omitted for passing instances |

String values are exact: control characters and other raw bytes from command output are preserved via standard JSON escaping.

Trimmed example:

```json
{
  "format_version": 1,
  "ok": false,
  "summary": {"files": 1, "tests": 2, "passed": 1, "failed": 1, "wall_seconds": 0.021},
  "files": [
    {
      "path": "greet.dats",
      "ok": false,
      "duration_seconds": 0.019,
      "setup_failure": null,
      "teardown_failures": [],
      "tests": [
        {
          "name": "greets [who=alice]",
          "index": 1,
          "ok": true,
          "duration_seconds": 0.010,
          "failures": [],
          "command": "echo \"hi alice\""
        },
        {
          "name": "greets [who=bob]",
          "index": 2,
          "ok": false,
          "duration_seconds": 0.009,
          "failures": ["stdout: expected output to contain \"hi bob\""],
          "command": "echo \"oops bob\"",
          "stdout": "oops bob\n",
          "stderr": ""
        }
      ]
    }
  ]
}
```

## JUnit XML format

The layout follows the common JUnit consumer conventions:

- Root `<testsuites tests="..." failures="..." time="...">` — totals over every suite (synthetic cases included). `time` is the wall time of the run in decimal seconds.
- One `<testsuite name="<file path as given>" tests="..." failures="..." time="...">` per file, in canonical order. The suite `time` is the sum of its cases' durations.
- One `<testcase classname="<file path>" name="<instance name>" time="...">` per test instance, in expansion order.
  - A **failed** instance carries `<failure message="<first failure message>">` with every failure message joined by newlines as the element text, plus the captured output in `<system-out>`/`<system-err>`. Passing instances carry none of these, keeping reports lean.
- A failed file-level **setup** adds one synthetic **first** case named `[setup]`. Each failed **teardown** command adds a synthetic **trailing** case named `[teardown]` (suffixed ` #1`, ` #2`, … only when more than one failed). Their `<failure>` carries the failure detail (message attribute) and the command plus detail (text). Captured output goes to `<system-out>`/`<system-err>`. Synthetic cases have no `time` attribute and are counted by the `tests`/`failures` attributes (see [Counts contract](#counts-contract)).
- Bytes that are illegal in XML 1.0 (e.g. `\x00`, `\x1b` from command output) are replaced with U+FFFD (`�`) so the document always stays well-formed. The JSON report preserves the exact bytes. Use it when fidelity matters.

Trimmed example:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<testsuites tests="3" failures="2" time="0.021">
  <testsuite name="greet.dats" tests="3" failures="2" time="0.019">
    <testcase classname="greet.dats" name="greets [who=alice]" time="0.010"></testcase>
    <testcase classname="greet.dats" name="greets [who=bob]" time="0.009">
      <failure message="stdout: expected output to contain &#34;hi bob&#34;">stdout: expected output to contain &#34;hi bob&#34;</failure>
      <system-out>oops bob&#xA;</system-out>
    </testcase>
    <testcase classname="greet.dats" name="[teardown]">
      <failure message="exit code 4">command: rm missing-file
exit code 4</failure>
    </testcase>
  </testsuite>
</testsuites>
```

## Stability

These reports are a consumption contract for CI and editor tooling:

- **Field names and document structure are stable** under a given `format_version`. A breaking change to the JSON format bumps `format_version`. The JUnit layout (elements, attributes, synthetic-case conventions) is kept equally stable.
- **Failure/assertion message text is human-readable and intentionally NOT stable** — do not parse it. Machine decisions belong on the structured fields (`ok`, counts, names, indices).
- **Durations vary** run to run. Treat every `time`/`*_seconds` value as informational.
- **Instance names** follow the CLI's printed names: the test's `desc` (or its command when there is no desc) plus the matrix label ` [k=v, ...]` for matrix instances.
