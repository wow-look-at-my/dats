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

	# Scratch space through TMPDIR, which the backends reach differently: bwrap
	# mounts a private writable /tmp, and seatbelt mounts nothing and denies
	# every write outside this file's temp tree. A command that scratches
	# through TMPDIR must work on both, so this is one of the tests every host
	# runs (.github/workflows/ci.yml, action-every-host).
	- desc: TMPDIR is writable inside the sandbox
	  cmd: |
		tmp="${TMPDIR:-/tmp}"
		printf 'scratch\n' > "$tmp/dats-tmpdir-probe"
		cat "$tmp/dats-tmpdir-probe"
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
		!stdout:
			- "modified inside the sandbox"

	# DIAGNOSTIC (temporary): this test itself already runs inside a seatbelt
	# sandbox on darwin. Applying a second, nested profile from here reproduces
	# go-toolchain#446's failure exactly. Captures the real exit code and stderr
	# instead of a boolean pass/fail, to learn what macOS actually denies.
	- desc: DIAGNOSTIC nested seatbelt attempt
	  cmd: |
		if command -v sandbox-exec >/dev/null 2>&1; then
			echo "sandbox-exec found at: $(command -v sandbox-exec)"
			sandbox-exec -p '(version 1)(allow default)' true
			echo "exit code (identical profile): $?"
			sandbox-exec -p '(version 1)(deny default)' true
			echo "exit code (deny default): $?"
			sandbox-exec -n no-network true
			echo "exit code (named profile): $?"
		else
			echo "sandbox-exec not on PATH"
		fi
