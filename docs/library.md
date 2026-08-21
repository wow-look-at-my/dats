# Using dats as a Go library

`github.com/wow-look-at-my/dats` is an importable package, not just a binary.
A Go program that wants to run `.dats` suites links it and calls `Run` — no
downloading a release, no bootstrapping a cache directory, no `exec.Command`
against a path that may or may not exist on the host.

The binary is a thin cobra wrapper over the same call, so the library is not a
second-class path: everything the CLI does — discovery, matrix expansion,
sandboxing, snapshots, reports, the exact bytes printed — is the library
doing it.

```go
import (
        "context"
        "fmt"
        "os"

        "github.com/wow-look-at-my/dats"
)

res, err := dats.Run(context.Background(), dats.Options{
        Paths:  []string{"dats"},
        Output: os.Stdout,
})
if err != nil {
        return fmt.Errorf("running dats suites: %w", err)
}
if !res.Ok() {
        return fmt.Errorf("dats suites failed: %d/%d passed", res.Passed, res.Passed+res.Failed)
}
```

## The error contract

`Run` returns an error only when the run itself could not be carried out:

- a path that does not exist, is not a `.dats` file, or is a directory with no
  suites in it,
- a file that fails to parse,
- a file that must be sandboxed on a host with no usable backend,
- an unknown `Sandbox.Mode`.

**Failing tests are not an error.** They are counted in the `Result`, and
`Result.Ok` is the verdict. That split is deliberate: a caller must never have
to match on a sentinel error or scrape output to tell "your suite is red" from
"dats could not run your suite" — those two demand different reactions, and
conflating them is how a broken harness gets reported as a test failure.

`Ok()` is not `Failed == 0`. A file whose teardown command failed fails the run
even when every test passed, exactly as the CLI's exit code does.

## Options

| Field | Default | Meaning |
|---|---|---|
| `Paths` | discover from the working directory | `.dats` files and directories to run |
| `Output` | `os.Stdout` | where the human-readable report goes |
| `Jobs` | `0` = one per logical CPU | run up to N commands concurrently, as `-jN` does; `1` runs one at a time. A negative is an error, not a synonym for serial |
| `Verbose` | off | print each command and its output |
| `Update` | off | rewrite snapshot goldens instead of failing |
| `KeepTemp` | off | keep (and print) each file's temp directory |
| `CoverDir` | none | `GOCOVERDIR` for every executed command |
| `Env` | none | extra `KEY=VALUE` entries for every executed command |
| `Sandbox` | auto | the isolation backend |

The zero `Options` value is a valid, sandboxed run of every suite under the
working directory.

### Sandbox: the zero value is ON

```go
Sandbox{}                                  // auto: bwrap, then seatbelt, then docker
Sandbox{Mode: runner.SandboxDocker, Image: "golang:1.25"}
Sandbox{Mode: runner.SandboxNone}          // host — the explicit opt-out
```

The zero value sandboxes, so a caller that says nothing about isolation gets
it. Running suite commands straight on the host is a decision someone makes on
purpose, never one the library makes on their behalf by defaulting to the
weaker thing. Individual files can still narrow this (`network: false`, or an
`image:` when `Image` is empty) — a file can only ever narrow what the caller
allowed, and a file asking to run unsandboxed is a parse error, not a
permission. A non-empty `Image` is the caller's pin and a file cannot displace
it; an empty one leaves the image to the file, then to the default.

### Env: additions, and deletions

`Env` entries are applied to every command the run executes — test instances
and file-level `setup`/`teardown` hooks alike — on top of the inherited
environment. A test's own `inputs.env` is applied after them, so a file always
wins over its caller.

An entry with an empty value (`"GOCACHEPROG="`) clears the inherited variable.
That is how a parent strips plumbing its children must not inherit: go-toolchain
runs suites with `GOCACHEPROG=` and `GOCACHE_STATS_SOCK=` cleared so a suite
command that shells out to `go` cannot re-enter the parent's build cache
daemon.

## Reports

`Result` renders the same machine-readable documents the `--report-junit` and
`--report-json` flags write (see [reports.md](reports.md) for the formats and
their stability contract):

```go
f, _ := os.Create("report.json")
defer f.Close()
if err := res.WriteJSON(f); err != nil { ... }
```

## Other entry points

- `dats.FindFiles(paths)` — the discovery rules on their own (extension check,
  recursion, hidden-entry skipping, dedup by absolute path), for a caller that
  wants to know what would run.
- `dats.Validate(paths)` — parse every resolved file without running anything;
  the library form of `dats syntax`.

## Lower-level packages

`runner`, `schema`, and `report` are public too and can be used directly: the
`runner.Runner` type, `schema.ParseFile`, `report.WriteJUnit`. They are the
guts, not the front door — a direct `runner.Runner` has a nil `Sandbox` (the
raw runner does not sandbox anything by itself; the safe default lives in
`Run`), no file discovery, and no totals. Reach for them when you are building
something dats itself does not do; use `Run` for everything else.
