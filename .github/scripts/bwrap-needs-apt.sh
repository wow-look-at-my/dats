#!/usr/bin/env bash
# Writes needed=true to GITHUB_OUTPUT when this Linux runner lacks bwrap and can
# install it through cached-apt, which drives apt-get and dpkg through sudo. Any
# other runner leaves the install to install-sandbox-backend.sh. See docs/action.md.
set -euo pipefail

needed=false
if [ "${RUNNER_OS:-}" = "Linux" ] &&
	! command -v bwrap >/dev/null 2>&1 &&
	command -v apt-get >/dev/null 2>&1 &&
	command -v dpkg >/dev/null 2>&1 &&
	command -v sudo >/dev/null 2>&1; then
	needed=true
fi

echo "sandbox: cached-apt install of bubblewrap needed=$needed"
echo "needed=$needed" >>"$GITHUB_OUTPUT"
