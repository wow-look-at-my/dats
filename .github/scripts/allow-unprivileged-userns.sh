#!/usr/bin/env bash
# Let an unprivileged process create a user namespace, which is what bubblewrap
# needs before it can build anything.
#
# TWO KNOBS, OPPOSITE POLARITY. This is the whole reason the file exists:
#
#   kernel.apparmor_restrict_unprivileged_userns   1 = RESTRICTED  -> write 0
#   kernel.unprivileged_userns_clone               1 = PERMITTED   -> write 1
#
# They read alike and mean the reverse of each other. A loop that "clears" both
# to 0 turns the second one OFF and DENIES the namespace it was called to
# allow -- measured on an ubuntu-24.04 runner, where both knobs exist and both
# start at 1: after that loop, bwrap inside a container reported "No permissions
# to create new namespace", caused by the step that was supposed to prevent it.
#
# Neither knob grants a capability and neither touches /proc masking. They
# govern one thing: whether an ordinary user may call unshare(CLONE_NEWUSER),
# which is the basis of a rootless sandbox rather than a hole in one.
set -euo pipefail

# name=value, in the polarity each knob actually uses.
for pair in \
	"kernel.apparmor_restrict_unprivileged_userns=0" \
	"kernel.unprivileged_userns_clone=1"; do
	knob="${pair%%=*}"
	want="${pair#*=}"
	path="/proc/sys/${knob//./\/}"
	if [ ! -f "$path" ]; then
		echo "   $knob: not present on this kernel"
		continue
	fi
	before="$(cat "$path")"
	if [ "$before" = "$want" ]; then
		echo "   $knob = $before already"
		continue
	fi
	# Best-effort: an unprivileged runner cannot write these, and that is not
	# on its own a reason to fail -- the caller's own probe decides whether a
	# sandbox can actually be built.
	sudo -n sysctl -w "$knob=$want" >/dev/null 2>&1 || true
	echo "   $knob: $before -> $(cat "$path") (wanted $want)"
done
