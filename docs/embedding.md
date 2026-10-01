# Embedding dats in a build tool

A build tool can link the dats library and run the suites of a repository as a phase of its own pipeline. It then ships no dats binary and no version to keep in step. `wow-look-at-my/go-toolchain` does this. A repository built that way can rely on the conventions below. They describe dats, not one consumer.

## Where suites live

Suites are non-hidden `*.dats` files under `dats/` at the module root. A directory with none makes the phase a silent no-op.

A repository with no module file of the host language still gets its suites run. Nothing was built. The handoff directory is empty. The suite exercises what is already in the tree. That is how a shell or TypeScript repository uses dats without a standalone binary.

## Indent with tabs

dats parses with `wow-look-at-my/yaml-fixed`, which inverts stock YAML. A space in the leading indentation is a parse error (`spaces cannot be used for indentation`). Spaces can only align after a tab. The sibling keys of a sequence item line up under the content after its `- ` marker: one tab of depth plus spaces of alignment.

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

The consumer exports one environment variable. It names a throwaway directory with copies of the binaries the pipeline just built. Each copy has its bare output name, plus `.exe` on Windows hosts. Tests exec binaries through that directory, not out of the build directory. An artifact in the build directory can be an APE that self-assimilates on first exec.

That directory sits **inside the module root**, under the build directory. A sandboxed command reaches only the working directory of the host. Docker mounts that directory and nothing else. bwrap binds the OS tool tree plus the working directory, with a private `/tmp` over it. A staging directory under `$TMPDIR` is invisible to both, so every suite fails its setup command. Version control ignores the build directory in the repositories this runs in. Staging there never dirties the tree.

## Suites are sandboxed

The phase does not pass `--no-sandbox`. That decision does not belong to the consumer. The handoff directory is read-only there, like the rest of the working directory. Nothing can declare otherwise. A test whose binary must write to itself copies it into the private `/tmp` first and execs the copy. A file cannot turn its own sandbox off. A suite that needs something specific of the docker backend names it with `image:`.

A consumer that bootstraps a language toolchain on every invocation must pin an image with that toolchain. Otherwise the docker fallback downloads one per command. bwrap and seatbelt ignore `image` and use the toolchain of the host.

## Execution order and selection

Suites run serially, with no `-j`. The report is then byte-deterministic, and staged copies never race their first exec.

The phase runs **every** discovered test. There is no filter, selection or skip mechanism, by design. dats itself has none either.

## The working directory

dats runs each command in the module root. Some consumers delete their build outputs on a failed run. A test that execs a pipeline command of that consumer from the module root can then delete the binaries the pipeline built. Such a test changes into its own outputs directory first.

## Snapshot goldens

Goldens live in `<suite>.snapshots/` next to the suite. They are committed. After an intentional output change, regenerate them with `dats --update test <dir>` and review the diff. A stale golden is a red run.
