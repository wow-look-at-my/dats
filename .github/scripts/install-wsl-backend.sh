#!/usr/bin/env bash
# Puts bubblewrap on an NT runner, inside WSL, and names the distro that holds
# it. NT can host no sandbox backend directly. See docs/action.md.
set -euo pipefail

DISTRO="${DATS_WSL_DISTRO:-Ubuntu}"
ROOTFS_URL="${DATS_WSL_ROOTFS_URL:-https://cloud-images.ubuntu.com/wsl/releases/24.04/current/ubuntu-noble-wsl-amd64-wsl.rootfs.tar.gz}"

# wsl.exe writes UTF-16LE by default, which reaches bash as text split by NUL
# bytes. WSL_UTF8 makes it write UTF-8, so its own errors stay readable here.
export WSL_UTF8=1

# Every wsl.exe call is bounded and reads no input. An unbounded one waits on a
# prompt no runner can answer, and the job then dies on its own timeout with no
# line naming the step that hung.
#
# Git Bash rewrites an argument that looks like a posix path into a Windows one
# before a non-MSYS program sees it, which turns the sandbox's own `/` into
# C:/Program Files/Git/. Only wsl.exe takes Linux paths, so only wsl.exe opts
# out: curl and the rest are native Windows programs that need the rewrite.
wsl_step() {
	local seconds="$1" what="$2" status=0
	shift 2
	MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*' \
		timeout "$seconds" wsl.exe "$@" </dev/null || status=$?
	if [ "$status" -eq 0 ]; then
		return 0
	fi
	if [ "$status" -eq 124 ]; then
		echo "sandbox: ${what} did not finish within ${seconds}s, so this runner cannot host the sandbox" >&2
	else
		echo "sandbox: ${what} failed with exit ${status}" >&2
	fi
	return "$status"
}

if ! command -v wsl.exe >/dev/null 2>&1; then
	echo "sandbox: wsl.exe is not on PATH, so this runner can host no Linux sandbox" >&2
	exit 1
fi

echo "sandbox: wsl version"
wsl_step 60 "wsl --version" --version || true
echo "sandbox: registered distributions"
wsl_step 60 "wsl --list" --list --quiet || true

if ! timeout 60 wsl.exe --list --quiet </dev/null | tr -d '\r' | grep -qi "^${DISTRO}$"; then
	# A pinned rootfs, imported directly. `wsl --install` fetches through the
	# Microsoft Store, which hangs unpredictably on a hosted runner, and a gate
	# that flakes is one a reader learns to ignore.
	work="$(cygpath -u "${RUNNER_TEMP:-/tmp}")/dats-wsl"
	mkdir -p "$work/root"
	echo "sandbox: downloading the ${DISTRO} rootfs"
	curl -fsSL --max-time 420 -o "$work/rootfs.tar.gz" "$ROOTFS_URL"
	echo "sandbox: importing the ${DISTRO} distribution"
	wsl_step 420 "importing ${DISTRO}" --import "$DISTRO" \
		"$(cygpath -w "$work/root")" "$(cygpath -w "$work/rootfs.tar.gz")" --version 2
fi

echo "sandbox: installing bubblewrap inside ${DISTRO}"
wsl_step 600 "installing bubblewrap" --distribution "$DISTRO" --user root -- \
	env DEBIAN_FRONTEND=noninteractive sh -c 'apt-get update && apt-get install -y bubblewrap'

# The install is this step's whole job, so a bwrap that cannot build a sandbox
# fails here rather than as a confusing "no usable sandbox backend" later.
echo "sandbox: exercising bubblewrap inside ${DISTRO}"
wsl_step 120 "exercising bubblewrap" --distribution "$DISTRO" --user root -- \
	bwrap --ro-bind / / --unshare-user --unshare-pid --dev /dev --proc /proc \
	--tmpfs /tmp --die-with-parent -- true

echo "DATS_WSL_DISTRO=${DISTRO}" >>"$GITHUB_ENV"
echo "sandbox: bubblewrap ready inside ${DISTRO}"
