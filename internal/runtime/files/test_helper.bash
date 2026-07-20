#!/usr/bin/env bash
# DATS Test Helper - Runtime support for generated BATS tests

# Get the directory containing this script
_test_helper_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Load bats-support and bats-assert
load "${_test_helper_dir}/test_helper/bats-support/load"
load "${_test_helper_dir}/test_helper/bats-assert/load"

# Default exit codes - users can override these in their own helper
export EXIT_SUCCESS=0
export EXIT_FAILURE=1

# assert_exit_code - Assert the exit code of the last command
# Usage: assert_exit_code <expected>
# The expected value can be a number or a variable like $EXIT_SUCCESS
assert_exit_code() {
    local expected="$1"
    if [[ "$status" -ne "$expected" ]]; then
        echo "Expected exit code: $expected"
        echo "Actual exit code: $status"
        echo "Output:"
        echo "$output"
        return 1
    fi
}

# Helper to create a temporary directory for test fixtures
# Returns the path in DATS_TMPDIR
setup_dats_tmpdir() {
    DATS_TMPDIR="$(mktemp -d)"
    export DATS_TMPDIR
}

# Helper to clean up temporary directory
teardown_dats_tmpdir() {
    if [[ -n "$DATS_TMPDIR" && -d "$DATS_TMPDIR" ]]; then
        rm -rf "$DATS_TMPDIR"
    fi
}

# run_with_stderr - Run a command and capture both stdout and stderr separately
# Usage: run_with_stderr <command> [args...]
# Sets: status, output (stdout), stderr_output
run_with_stderr() {
    local tmpdir
    tmpdir="$(mktemp -d)"

    # Run command, capturing stdout and stderr separately
    set +e
    "$@" > "${tmpdir}/stdout" 2> "${tmpdir}/stderr"
    status=$?
    set -e

    output="$(cat "${tmpdir}/stdout")"
    stderr_output="$(cat "${tmpdir}/stderr")"

    # Also set lines array for compatibility
    mapfile -t lines < "${tmpdir}/stdout"
    mapfile -t stderr_lines < "${tmpdir}/stderr"

    rm -rf "$tmpdir"
}

# assert_stderr - Assert something about stderr (use after run_with_stderr)
# Usage: assert_stderr --partial <pattern>
assert_stderr() {
    local partial=""
    local pattern=""

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --partial)
                partial=1
                shift
                ;;
            *)
                pattern="$1"
                shift
                ;;
        esac
    done

    if [[ -n "$partial" ]]; then
        if [[ "$stderr_output" != *"$pattern"* ]]; then
            echo "Expected stderr to contain: $pattern"
            echo "Actual stderr: $stderr_output"
            return 1
        fi
    else
        if [[ "$stderr_output" != "$pattern" ]]; then
            echo "Expected stderr: $pattern"
            echo "Actual stderr: $stderr_output"
            return 1
        fi
    fi
}

# refute_stderr - Assert stderr does NOT contain pattern
# Usage: refute_stderr --partial <pattern>
refute_stderr() {
    local partial=""
    local pattern=""

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --partial)
                partial=1
                shift
                ;;
            *)
                pattern="$1"
                shift
                ;;
        esac
    done

    if [[ -n "$partial" ]]; then
        if [[ "$stderr_output" == *"$pattern"* ]]; then
            echo "Expected stderr to NOT contain: $pattern"
            echo "Actual stderr: $stderr_output"
            return 1
        fi
    else
        if [[ "$stderr_output" == "$pattern" ]]; then
            echo "Expected stderr to NOT equal: $pattern"
            echo "Actual stderr: $stderr_output"
            return 1
        fi
    fi
}
