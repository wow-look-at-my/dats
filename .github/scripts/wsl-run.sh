#!/bin/sh
# Runs the dats APE inside a WSL distribution, for install-wsl-backend.sh's distro.
set -eu

seconds="$1"
bin="$2"
shift 2

# WSL registers a binfmt handler for the MZ header.
for f in /proc/sys/fs/binfmt_misc/WSLInterop*; do
	if [ -e "$f" ]; then echo 0 >"$f"; fi
done

# Set, not inherited: WSL appends the Windows entries and omits /usr/bin.
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH

# shellcheck disable=SC2016
exec timeout "$seconds" bash -c '"$0" "$@"' "$bin" "$@"
