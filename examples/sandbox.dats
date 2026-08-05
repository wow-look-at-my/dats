# Sandboxing.
#
# Test commands are sandboxed by default (bubblewrap, falling back to docker):
# the host filesystem is readable but not writable, and only this file's own
# temp directory -- fixtures, {outputs.X}, {shared.X} -- can be written. Nothing
# in a .dats file has to change for that: the tests below are ordinary tests.
#
# The optional file-level `sandbox` block narrows the sandbox for one file.
# Write `sandbox: false` instead to opt a file out entirely, for commands that
# genuinely need the host. Either way the CLI is the outer bound: under
# --no-sandbox this block is inert and everything here still runs (and passes).
sandbox:
  network: false # this file's commands need no network, so they get none

tests:
  # Fixtures and output files live in the sandbox's writable temp directory,
  # so every placeholder and assertion behaves exactly as it does unsandboxed.
  - desc: fixtures and output files work the same inside the sandbox
    cmd: cat {inputs.in.txt} > {outputs.out.txt}
    inputs:
      files:
        in.txt: sandboxed content
    outputs:
      files:
        out.txt:
          match:
            - sandboxed content

  # The file-wide shared directory is writable too -- it is part of the same
  # temp tree -- so setup-style file generation works under a sandbox.
  - desc: the shared directory is writable
    cmd: echo generated > {shared.note.txt} && cat {shared.note.txt}
    outputs:
      stdout:
        - generated

  # The working directory is bind-mounted read-only, so a plain read still
  # works but a write to it would fail. inputs.copy is the read-write way in:
  # it copies an existing host file into the sandbox's writable temp
  # directory before the command runs, so this test can freely modify its
  # own copy without touching the real fixture on disk.
  - desc: inputs.copy pulls a host file in, writable
    inputs:
      copy:
        source.txt: host-files/readonly-source.txt
    cmd: |
      echo "modified inside the sandbox" >> {inputs.source.txt}
      cat {inputs.source.txt}
    outputs:
      stdout:
        - "the source lives on the host"
        - "modified inside the sandbox"

  # The host fixture itself is read-only from inside the sandbox (and never
  # touched by the copy above, which wrote to the temp directory instead).
  # cmd runs in dats' own working directory (unlike inputs.copy sources,
  # which resolve relative to this .dats file), so the path is spelled from
  # the repo root -- the directory `just test` runs `dats examples/` from.
  - desc: the host fixture the copy came from is untouched
    cmd: cat examples/host-files/readonly-source.txt
    outputs:
      stdout:
        - "the source lives on the host"
      "!stdout":
        - "modified inside the sandbox"
