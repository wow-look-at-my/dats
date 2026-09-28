# Embedding dats in a build tool

A build tool can link the dats library and run a repository's suites as a
phase of its own pipeline, instead of shipping a dats binary and a version to
keep in step. `wow-look-at-my/go-toolchain` does this, and the conventions
below are what a repository built that way can rely on. They are stated here
because they describe dats, not any one consumer.

## Where suites live

Suites are non-hidden `*.dats` files under `dats/` at the module root. A
directory with none makes the phase a silent no-op.

A repository with no module file of the host language still gets its suites
run: nothing was built, the handoff directory is empty, and the suite
exercises what is already in the tree. That is how a shell or TypeScript
repository uses dats without fetching a standalone binary.

## Indent with tabs

dats parses with `wow-look-at-my/yaml-fixed`, which inverts stock YAML. A
space in the leading indentation is a parse error (`spaces cannot be used for
indentation`), and spaces may only align after a tab. A sequence item's
sibling keys therefore line up under the content after its `- ` marker: one
tab of depth plus two spaces of alignment.

```
tests:
> - desc: something
> ..cmd: echo hi
> ..outputs:
> > stdout:
> > > - hi
```

(`>` a tab, `.` a space.)

## The handoff directory

The consumer exports one environment variable naming a throwaway directory
that holds copies of the binaries the pipeline just built, each under its bare
output name (plus `.exe` on Windows hosts). Tests exec binaries through that
directory rather than out of the build directory, because an artifact there
may be an APE that self-assimilates on first exec.

That directory sits **inside the module root**, under the build directory. A
sandboxed command reaches only the working directory of the host: docker
mounts that and nothing else, and bwrap binds the OS tool tree plus the
working directory with a private `/tmp` over it. A staging directory under
`$TMPDIR` is invisible to both, so every suite would fail its setup command.
The build directory is ignored by version control in the repositories this
runs in, so staging there never dirties the tree.

## Suites are sandboxed

The phase does not pass `--no-sandbox`, and that is not the consumer's call to
make. The handoff directory is read-only there, like the rest of the working
directory, and nothing can declare otherwise. A test whose binary must write
to itself copies it into the private `/tmp` first and execs the copy. A suite
whose commands genuinely need the host says so per file with `sandbox: false`.
One that needs something specific of the docker backend names it with
`image:`.

A consumer that bootstraps a language toolchain on every invocation should pin
an image carrying that toolchain, or the docker fallback downloads one per
command. bwrap and seatbelt ignore `image` and use the host's.

## Execution order and selection

Suites run serially, with no `-j`, so the report is byte-deterministic and
staged copies never race their first exec.

The phase runs **every** discovered test. There is no filtering, selection or
skip mechanism, by design: dats itself has none either.

## The working directory

dats runs each command in the module root. A consumer that deletes its build
outputs on a failed run will delete the binaries the pipeline just built if a
test execs one of its own pipeline commands from there. Such a test changes
into its own outputs directory first.

## Snapshot goldens

Goldens live in `<suite>.snapshots/` next to the suite, and are committed.
Regenerate them after an intentional output change with `dats --update test
<dir>` and review the diff. A stale golden is a red run.
