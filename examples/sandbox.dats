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
