#!/usr/bin/env bash
# Puts bubblewrap on an NT runner, inside WSL, and names the distro that holds
# it. NT can host no sandbox backend directly. See docs/action.md.
set -euo pipefail

DISTRO="${DATS_WSL_DISTRO:-Ubuntu}"

# wsl.exe writes UTF-16LE, which reaches bash as text split by NUL bytes.
wsl_out() {
	wsl.exe "$@" 2>&1 | tr -d '\000\r'
}

if ! command -v wsl.exe >/dev/null 2>&1; then
	echo "sandbox: wsl.exe is not on PATH, so this runner can host no Linux sandbox" >&2
	exit 1
fi

echo "sandbox: wsl version"
wsl_out --version || true
echo "sandbox: registered distributions"
wsl_out --list --quiet || true

if ! wsl_out --list --quiet | grep -qi "^${DISTRO}$"; then
	echo "sandbox: installing the ${DISTRO} distribution"
	# --no-launch registers the distribution without the interactive first-run
	# account setup, which no runner can answer.
	wsl.exe --install --no-launch --distribution "$DISTRO"
fi

echo "sandbox: installing bubblewrap inside ${DISTRO}"
wsl.exe --distribution "$DISTRO" --user root -- \
	sh -c 'apt-get update && apt-get install -y bubblewrap'

# The install is this step's whole job, so a bwrap that cannot build a sandbox
# fails here rather than as a confusing "no usable sandbox backend" later.
echo "sandbox: exercising bubblewrap inside ${DISTRO}"
wsl.exe --distribution "$DISTRO" --user root -- \
	bwrap --ro-bind / / --unshare-user --unshare-pid --dev /dev --proc /proc \
	--tmpfs /tmp --die-with-parent -- true

echo "DATS_WSL_DISTRO=${DISTRO}" >>"$GITHUB_ENV"
echo "sandbox: bubblewrap ready inside ${DISTRO}"
