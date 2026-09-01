#!/usr/bin/env bash
# Installs the sandbox backend dats needs on this runner, so a caller never has
# to reach for --no-sandbox to work around infrastructure. See docs/action.md.
set -euo pipefail

case "${RUNNER_OS:-Linux}" in
	macOS)
		# seatbelt is /usr/bin/sandbox-exec, shipped with macOS. Nothing to install.
		if [ -x /usr/bin/sandbox-exec ]; then
			echo "sandbox: seatbelt already present at /usr/bin/sandbox-exec"
			exit 0
		fi
		echo "sandbox: /usr/bin/sandbox-exec is missing on a macOS runner" >&2
		exit 1
		;;
	Windows)
		# NT has no backend of its own: bwrap is Linux, seatbelt is macOS, and the
		# runner's docker daemon serves windows containers. WSL is the Linux this
		# host does have, so the backend goes there and the APE runs its Linux
		# payload inside it. run-dats.ts reads the distro name this writes.
		exec bash "$(dirname "$0")/install-wsl-backend.sh"
		;;
esac

if command -v bwrap >/dev/null 2>&1; then
	echo "sandbox: bubblewrap already present at $(command -v bwrap)"
	exit 0
fi

# A container job usually runs as root with no sudo binary at all, so asking for
# sudo unconditionally fails an install plain apt-get would have completed.
SUDO=""
if [ "$(id -u)" -ne 0 ]; then
	if ! command -v sudo >/dev/null 2>&1; then
		echo "sandbox: bubblewrap is missing and this job is neither root nor able to sudo" >&2
		echo "sandbox: install bubblewrap in the runner image, or use a runner with a docker daemon (dats falls back to docker)" >&2
		exit 1
	fi
	SUDO="sudo"
fi

if command -v apt-get >/dev/null 2>&1; then
	$SUDO apt-get update
	$SUDO apt-get install -y bubblewrap
elif command -v dnf >/dev/null 2>&1; then
	$SUDO dnf install -y bubblewrap
elif command -v apk >/dev/null 2>&1; then
	$SUDO apk add --no-cache bubblewrap
else
	echo "sandbox: no supported package manager found (looked for apt-get, dnf, apk)" >&2
	echo "sandbox: install bubblewrap in the runner image, or pass --no-sandbox in args" >&2
	exit 1
fi

# The install is the step's whole job, so a bwrap that is still missing is a
# failure here rather than a confusing "no usable sandbox backend" later.
if ! command -v bwrap >/dev/null 2>&1; then
	echo "sandbox: the package installed but bwrap is still not on PATH" >&2
	exit 1
fi

echo "sandbox: installed bubblewrap at $(command -v bwrap)"
