#!/usr/bin/env bash
# Run a dats suite on the self-hosted fleet, asserting what dats owes in
# WHICHEVER state the fleet is in.
#
# The job this backs used to run the suite and nothing else, which made it an
# assertion about the FLEET: bubblewrap needs an unprivileged user namespace,
# the slim runner grants one through its hook's `seccomp.userns` opt-in, and
# while that opt-in is not deployed the kernel refuses bwrap before dats gets
# anywhere near a sandbox. dats cannot set a container's seccomp profile, so on
# an un-opted-in fleet that job could not pass whatever dats did.
#
# Both branches below assert something real about dats, and neither is a pass
# handed out for free:
#
#   namespace available -> run the suite. Unchanged, full strength.
#   namespace refused   -> dats must FAIL CLOSED: non-zero, naming the refusal
#                          and naming the opt-out. A dats that ran the suite
#                          unsandboxed here, or died without saying why, fails
#                          the job.
#
# This does NOT leave the masked-/proc fallback ungated. That is pinned against
# a real kernel refusal by runner/sandbox_maskedproc_linux_test.go and
# runner/sandbox_procgate_linux_test.go in the `test` job, which run on every
# push. What this job adds is end-to-end coverage on the fleet a consumer
# actually uses -- and it keeps adding it the moment the opt-in deploys.
set -uo pipefail

DATS="${1:?usage: fleet-sandbox.sh <dats-binary> <suite>}"
SUITE="${2:?usage: fleet-sandbox.sh <dats-binary> <suite>}"

# The narrowest probe for the one capability in question. Not dats' own probe:
# this must answer "can this container make a user namespace at all", nothing
# about /proc, mounts, or which shape dats would settle on.
if bwrap --ro-bind / / --unshare-user true 2>/dev/null; then
	echo "fleet: unprivileged user namespaces available -- running the suite for real"
	exec "$DATS" test "$SUITE"
fi

echo "::warning::fleet: this runner cannot create an unprivileged user namespace."
echo "The slim fleet grants it through the gha-runner hook's seccomp.userns opt-in."
echo "Asserting dats fails CLOSED here instead of running the suite unsandboxed."

out="$("$DATS" test "$SUITE" 2>&1)"
status=$?
echo "$out"

if [ "$status" -eq 0 ]; then
	echo "::error::dats exited 0 with no usable sandbox backend. A run that cannot"
	echo "::error::isolate must fail, never quietly execute the suite on the host."
	exit 1
fi

fail=0
case "$out" in
*"no usable sandbox backend"*) ;;
*)
	echo "::error::dats failed for some reason OTHER than the missing namespace."
	echo "::error::That is a real failure and this branch must not absorb it."
	fail=1
	;;
esac
# The message is the only thing standing between an operator and an afternoon
# spent reinstalling a bubblewrap that was already there, so it is asserted.
case "$out" in
*"--no-sandbox"*) ;;
*)
	echo "::error::the failure did not name --no-sandbox. A refusal an operator"
	echo "::error::cannot act on is barely better than a silent one."
	fail=1
	;;
esac
[ "$fail" -ne 0 ] && exit 1

echo "fleet: dats failed closed with an actionable message, as required."
