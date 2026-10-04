#!/usr/bin/env bash
# Run the sandbox suite inside an unprivileged container whose /proc is masked -- the shape the org's slim CI fleet has.
set -euo pipefail

SUITE="${1:-examples/sandbox.dats}"
IMAGE="${DATS_SANDBOX_TEST_IMAGE:-debian:stable-slim}"

echo "== host: let an unprivileged process create a user namespace"
# The setting is HOST-WIDE and a container inherits it.
"$(dirname "$0")/allow-unprivileged-userns.sh"

echo "== the container the suite will run in"
# No --privileged and no added capability.
run_in_container() {
	docker run --rm \
		--security-opt seccomp=unconfined \
		--security-opt apparmor=unconfined \
		-v "$PWD:/w" -w /w \
		"$IMAGE" sh -c "$1"
}

echo "== negative control: /proc must be MASKED in here"
# If this ever passes, the container.
masked=$(run_in_container '
	if [ -w /proc/sysrq-trigger ]; then echo UNMASKED; else echo masked; fi
')
echo "   /proc/sysrq-trigger: $masked"
if [ "$masked" != "masked" ]; then
	echo "::error::/proc is NOT masked in this container, so this job is no longer"
	echo "::error::testing the fleet's shape. Refusing to report a pass for it."
	exit 1
fi

echo "== running the suite"
out=$(run_in_container '
	apt-get update -qq >/dev/null 2>&1
	apt-get install -y -qq bubblewrap >/dev/null 2>&1
	./build/dats test '"$SUITE"' 2>&1
') || {
	echo "$out"
	echo "::error::the suite failed in a masked unprivileged container."
	exit 1
}
echo "$out"

echo "== the sandbox must be the one this case is about"
# A pass on a PRIVATE procfs would mean the mask never applied, so the fallback
# under test never ran. dats announces the shape it settled on; require it.
case "$out" in
*"bwrap (shared /proc)"*)
	echo "   dats took the read-only /proc bind, as a masked container requires."
	;;
*)
	echo "::error::dats did not report the shared-/proc sandbox. Either the mask"
	echo "::error::did not apply or a different backend ran, so the fallback this"
	echo "::error::job exists for was not exercised."
	exit 1
	;;
esac
