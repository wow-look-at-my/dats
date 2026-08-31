#!/bin/sh
# Runs the dats APE inside a WSL distribution, for install-wsl-backend.sh's
# distro. A checked-in file, invoked with plain arguments only: wsl.exe joins
# its argv into one command line that the Linux side parses again, so a script
# handed to `sh -c` through that boundary loses its own quoting.
set -eu

seconds="$1"
bin="$2"
shift 2

# WSL registers a binfmt handler for the MZ header, and the APE carries one, so
# without this the kernel hands the file to Windows and dats probes the host.
for f in /proc/sys/fs/binfmt_misc/WSLInterop*; do
	if [ -e "$f" ]; then echo 0 >"$f"; fi
done

# Set, not inherited: WSL appends the Windows entries and omits /usr/bin, so
# dats found no bwrap and did find the host's docker.exe.
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH

# A shell reads the APE header and execs the payload. execve refuses it. bash
# must receive $0 and $@ unexpanded, so the quotes are single on purpose.
# shellcheck disable=SC2016
exec timeout "$seconds" bash -c '"$0" "$@"' "$bin" "$@"
