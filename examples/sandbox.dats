# Sandboxing.
#
# Test commands are sandboxed by default (bubblewrap, falling back to docker):
# the host filesystem is readable but not writable, and only this file's own
# temp directory -- fixtures, {outputs.X}, {shared.X} -- can be written. Nothing
# in a .dats file has to change for that: the tests below are ordinary tests.
#
# The optional file-level `sandbox` block narrows the sandbox for one file. It
# cannot widen it, and there is no way for a file to switch its sandbox off:
# commands that need the host need a run someone started with --no-sandbox. The
# CLI is the outer bound, so under --no-sandbox this block is inert and
# everything here still runs (and passes).
sandbox:
	network: false # this file's commands need no network, so they get none

# Every command runs here, resolved against this file's own directory, so the
# paths below read the same whatever directory the run started in. It replaces
# a cd inside cmd, which is a parse error.
workdir: .

tests:
	# Fixtures and output files live in the sandbox's writable temp directory,
	# so every placeholder and assertion behaves exactly as it does unsandboxed.
	- desc: fixtures and output files work the same inside the sandbox
	  cmd: cp {inputs.in.txt} {outputs.out.txt}
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
	  cmd: echo generated | tee {shared.note.txt}
	  outputs:
		stdout:
			- generated

	# Scratch space through TMPDIR, which the backends reach differently: bwrap
	# mounts a private writable /tmp, and seatbelt mounts nothing and denies
	# every write outside this file's temp tree. A command that scratches
	# through TMPDIR must work on both, so this is one of the tests every host
	# runs (.github/workflows/ci.yml, action-every-host).
	- desc: TMPDIR is writable inside the sandbox
	  cmd: |
		tmp="${TMPDIR:-/tmp}"
		printf 'scratch\n' | tee "$tmp/dats-tmpdir-probe"
	  outputs:
		stdout:
			- scratch

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
		echo "modified inside the sandbox" | tee -a {inputs.source.txt}
		cat {inputs.source.txt}
	  outputs:
		stdout:
			- "the source lives on the host"
			- "modified inside the sandbox"

	# The host fixture itself is read-only from inside the sandbox (and never
	# touched by the copy above, which wrote to the temp directory instead).
	# The file's workdir puts cmd in this .dats file's own directory, the same
	# place inputs.copy sources resolve against, so both spell the path alike.
	- desc: the host fixture the copy came from is untouched
	  cmd: cat host-files/readonly-source.txt
	  outputs:
		stdout:
			- "the source lives on the host"
		!stdout:
			- "modified inside the sandbox"
