# Snapshot (golden-file) assertions: outputs.snapshot asserts that captured
# stdout (and/or stderr) byte-matches a golden file stored in the
# snapshot.snapshots/ directory next to this file (one golden per instance
# per enabled stream, named NNN-<slug>.<stream>.golden). Create or refresh
# the goldens with `dats --update`; framework temp paths in the output are
# normalized to {testdir}/{shareddir}/{tmproot} tokens, so the committed
# goldens are stable across runs and machines.
tests:
  # Boolean shorthand: snapshot stdout against
  # snapshot.snapshots/001-snapshots-stdout.stdout.golden
  - desc: snapshots stdout
    cmd: printf 'alpha\nbeta\ngamma\n'
    outputs:
      snapshot: true

  # Per-stream form: each enabled stream gets its own golden file
  - desc: snapshots both streams
    cmd: echo "to stdout"; echo "to stderr" >&2
    outputs:
      snapshot:
        stdout: true
        stderr: true

  # stderr-only snapshot, composed with ordinary assertions
  - desc: snapshots stderr only
    cmd: 'echo "warning: check the flux capacitor" >&2'
    outputs:
      stderr:
        - "warning"
      snapshot:
        stderr: true

  # Matrix test: every instance gets its own golden -- the matrix label is
  # part of the file name slug (004-greets-who-alice..., 005-greets-who-bob...)
  - desc: greets
    cmd: echo "hello, {matrix.who}!"
    matrix:
      who: [alice, bob]
    outputs:
      snapshot: true

  # Output containing fixture paths: the golden stores the {testdir} token,
  # so it matches on every run even though the temp directory changes
  - desc: prints a fixture path
    cmd: echo "reading {inputs.data.txt}" && cat {inputs.data.txt}
    inputs:
      files:
        data.txt: "fixture content"
    outputs:
      snapshot: true
