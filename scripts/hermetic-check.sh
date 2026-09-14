#!/bin/sh
# Run the test suite against a throwaway HOME and report any state it created
# outside its own temporary directories.
#
# The point is to keep "the tests do not touch the environment" a property that
# is checked rather than claimed: a test that reaches for the real home
# directory, the real state directory, or a global config fails here instead of
# failing quietly on somebody else's machine.
#
# The Go toolchain keeps its own telemetry counters under the home directory and
# has no portable way to be told otherwise from the environment alone (the
# `go telemetry` setting is persisted per installation, not per run), so that one
# directory is excluded by name. Anything else left behind is a test's doing.
set -eu

# The scratch home is kept on failure so the offending paths can be inspected.
scratch="$(mktemp -d)"
keep=0
cleanup() {
	if [ "$keep" -eq 0 ]; then rm -rf "$scratch"; else echo "scratch kept at $scratch" >&2; fi
}
trap cleanup EXIT
home="$scratch/home"
mkdir -p "$home"

# Every dshctl variable is pointed into the throwaway home. A test that reads one
# of them from the surrounding environment instead of from its own fixture
# therefore writes inside $home, where the check below sees it — which turns
# "the suite is hermetic" into "the suite leaves no trace anywhere else".
#
# GOCACHE is pointed outside the home as well. The toolchain keeps its build
# cache under the home directory by default — ~/.cache on Linux, ~/Library/Caches
# on macOS — and that bookkeeping belongs to the compiler rather than to the
# tests. A home the suite never writes to is a stronger claim than one carrying a
# list of tolerated exceptions, so the cache is moved rather than excused.
env HOME="$home" \
    GOCACHE="$scratch/gocache" \
    DSH_HOME="$home/.dsh" \
    DSHCTL_STATE_DIR="$home/.dsh/dshctl" \
    DSHCTL_CONFIG="$home/.dsh/dshctl/config.json" \
    DSH_REPO_DIR="$home/repo-from-environment" \
    DSH_LOG_FILE="$home/log-from-environment.log" \
    go test "$@" ./...

stray="$(
	find "$home" -mindepth 1 -maxdepth 1 \
		! -name '.dsh' \
		! -name 'Library' \
		! -name '.config' \
		! -name 'repo-from-environment' \
		! -name 'log-from-environment.log' \
		-print
)"
# The two environment-pointed paths may legitimately be created by the binary
# under test, but nothing may be left inside them: they belong to the environment,
# not to a test.
for env_path in "$home/repo-from-environment" "$home/log-from-environment.log"; do
	[ -e "$env_path" ] || continue
	stray="$stray
$env_path"
done
# Inside the Go toolchain's own directories, only its own files may appear.
for toolchain_dir in "$home/Library/Application Support/go" "$home/.config/go"; do
	[ -d "$toolchain_dir" ] || continue
	unexpected="$(find "$toolchain_dir" -mindepth 1 -maxdepth 1 ! -name 'telemetry' -print)"
	if [ -n "$unexpected" ]; then
		stray="$stray
$unexpected"
	fi
done

if [ -n "$stray" ]; then
	echo "tests created files in a real home directory:" >&2
	echo "$stray" >&2
	keep=1
	exit 1
fi
echo "hermetic: no state was created outside the tests' own temporary directories"
