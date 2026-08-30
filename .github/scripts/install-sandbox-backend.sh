#!/usr/bin/env bash
# Installs the sandbox backend dats needs on this runner, so a caller never has
# to reach for --no-sandbox to work around infrastructure. See docs/action.md.
set -euo pipefail

# The Windows branch's knobs. The daemon binds every interface because WSL is
# its own network namespace; the loopback address is what the host reaches.
WSL_DISTRO="Ubuntu-24.04"
WSL_DOCKER_HOST="tcp://127.0.0.1:2375"
WSL_DOCKER_WAIT_SECONDS=180

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
		# dats has no native Windows backend, so the sandbox is docker -- and it has to be a
		# daemon serving LINUX containers, which the runner's own daemon is not.
		if docker info --format '{{.OSType}}' 2>/dev/null | grep -q linux; then
			echo "sandbox: docker already serves linux containers"
			exit 0
		fi
		echo "sandbox: the local daemon serves no linux containers; starting one under WSL"
		powershell -NonInteractive -Command "wsl --set-default-version 1; wsl --install --no-launch --distribution $WSL_DISTRO"
		wsl -d "$WSL_DISTRO" -u root -- bash -c "apt-get update && apt-get install -y docker.io"
		# setsid + disown, or the daemon dies with the step that started it.
		wsl -d "$WSL_DISTRO" -u root -- bash -c \
			"setsid nohup dockerd --host=tcp://0.0.0.0:2375 --iptables=false --bridge=none >/var/log/dockerd.log 2>&1 </dev/null & disown"
		export DOCKER_HOST="$WSL_DOCKER_HOST"
		if [ -n "${GITHUB_ENV:-}" ]; then
			echo "DOCKER_HOST=$WSL_DOCKER_HOST" >> "$GITHUB_ENV"
		fi
		waited=0
		while [ "$waited" -lt "$WSL_DOCKER_WAIT_SECONDS" ]; do
			if docker info >/dev/null 2>&1; then
				break
			fi
			sleep 5
			waited=$((waited + 5))
		done
		if ! docker info --format '{{.OSType}}' | grep -q linux; then
			echo "sandbox: the WSL daemon never answered on $WSL_DOCKER_HOST" >&2
			exit 1
		fi
		echo "sandbox: linux docker daemon from WSL at $WSL_DOCKER_HOST"
		exit 0
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
		echo "sandbox: install bubblewrap in the runner image, or pass --no-sandbox in args" >&2
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
