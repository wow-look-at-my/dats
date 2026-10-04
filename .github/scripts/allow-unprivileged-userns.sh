#!/usr/bin/env bash
# Let an unprivileged process create a user namespace, which is what bubblewrap needs before it can build anything.
set -euo pipefail

if ! command -v sysctl >/dev/null 2>&1; then
	echo "   sysctl is not installed, so neither knob can be read or set here"
	exit 0
fi

# $1 is the sysctl. $2 is the value that means "allowed" FOR THAT KNOB.
allow() {
	knob=$1
	want=$2

	if ! before=$(sysctl -n "$knob" 2>/dev/null); then
		echo "   $knob: not present on this kernel"
		return
	fi
	if [ "$before" = "$want" ]; then
		echo "   $knob = $before already"
		return
	fi

	# Best-effort: an unprivileged runner cannot write these.
	sudo -n sysctl -w "$knob=$want" >/dev/null 2>&1 || true
	echo "   $knob: $before -> $(sysctl -n "$knob") (wanted $want)"
}

allow kernel.apparmor_restrict_unprivileged_userns 0
allow kernel.unprivileged_userns_clone 1
