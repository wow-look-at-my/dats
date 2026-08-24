#!/usr/bin/env bash
# Run the sandbox suite inside an unprivileged container whose /proc is masked
# -- the shape the org's slim CI fleet has, built here instead of borrowed.
#
# WHAT THIS IS NOT: `--security-opt seccomp=unconfined` relaxes SYSCALL
# FILTERING. It is not `--security-opt systempaths=unconfined`, which unmasks
# /proc and hands a container root a writable /proc/sysrq-trigger, i.e. a host
# reboot. Those two are one word apart and opposite in consequence. The fleet
# grants the first, narrowly (docker's default profile plus an ungated allow for
# unshare/clone/clone3/setns/mount/umount2/pivot_root -- webhook-runner's
# `seccomp.userns`); it grants the second to nobody, and neither does this. The
# assertions below FAIL the job if /proc ever stops being masked here, so this
# can never quietly become a test of the easy case.
#
# WHY NOT THE FLEET ITSELF: it was the fleet, and a merge gate that depends on
# another repo's DEPLOYED state is a gate that can be frozen by that repo. It
# was: the slim runners' `seccomp.userns` opt-in sat undeployed behind a held
# reload gate, dats could not sandbox there whatever dats did, and every dats
# pull request was blocked on a webhooks deploy that was itself waiting on a
# dats release. Ownership now sits where each half can be acted on -- dats
# proves the MECHANISM here, and webhooks' own gha-runner smoke test proves the
# IMAGE, running this same binary on the real thing.
set -euo pipefail

SUITE="${1:-examples/sandbox.dats}"
IMAGE="${DATS_SANDBOX_TEST_IMAGE:-debian:stable-slim}"

echo "== host: clear the AppArmor restriction on unprivileged user namespaces"
# ubuntu-24.04 sets this to 1, and it is HOST-WIDE: a container inherits the
# refusal, so bwrap would fail inside one for a reason that has nothing to do
# with the container's own configuration. The runner's own CI clears the same
# knob (README.md "test: installing bubblewrap and clearing the restriction").
for knob in kernel.apparmor_restrict_unprivileged_userns kernel.unprivileged_userns_clone; do
	path="/proc/sys/${knob//./\/}"
	if [ -f "$path" ]; then
		echo "   $knob = $(cat "$path"), clearing"
		sudo sysctl -w "$knob=0" >/dev/null || true
	else
		echo "   $knob: not present on this kernel"
	fi
done

echo "== the container the suite will run in"
# No --privileged and no added capability. What is relaxed is the two MAC
# layers that refuse an unprivileged process the namespace and the mounts
# inside it: seccomp (the axis the fleet relaxes, via `seccomp.userns`) and, on
# an Ubuntu host, the docker-default AppArmor profile, which denies mount
# regardless of what seccomp allows. The fleet needs only the first because its
# hosts are not running that profile.
#
# Neither touches the masking. /proc stays exactly as docker leaves it, and the
# negative control below fails the job if that ever stops being true -- which is
# what keeps this a faithful stand-in rather than an easier test wearing its name.
run_in_container() {
	docker run --rm \
		--security-opt seccomp=unconfined \
		--security-opt apparmor=unconfined \
		-v "$PWD:/w" -w /w \
		"$IMAGE" sh -c "$1"
}

echo "== negative control: /proc must be MASKED in here"
# If this ever passes, the container stopped being the case under test and the
# suite below would be exercising a private procfs it can actually mount.
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
