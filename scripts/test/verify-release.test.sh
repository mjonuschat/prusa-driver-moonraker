#!/bin/sh
# Tests scripts/verify-release.sh. Run from the repository root.
set -u
# shellcheck source=scripts/test/lib.sh
. "$(dirname "$0")/lib.sh"

script=$PWD/scripts/verify-release.sh
assert_executable "verify-release.sh exists and is executable" "$script"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

manifest=$tmp/module.yaml
changelog=$tmp/CHANGELOG.md

set_fixture() {
	printf 'name: machines\nversion: %s\nkind: asset\n' "$1" >"$manifest"
	printf '# Changelog\n\nintro text\n\n## %s\n\n- one\n' "$2" >"$changelog"
	if [ $# -ge 3 ]; then
		printf '\n## %s\n\n- two\n' "$3" >>"$changelog"
	fi
}

# run <args...>: runs the script with the fixture files, in a repository whose
# tag commit is on main.
repo=$tmp/repo
git_temp_repo "$repo"
(cd "$repo" && git commit --allow-empty -m "feat: base" --quiet && git branch -M main)

run() {
	(cd "$repo" && MODULE_YAML=$manifest CHANGELOG=$changelog MAIN_REF=main TAG_COMMIT=HEAD "$script" "$@")
}

set_fixture 0.2.0 "0.2.0 - 2026-12-01"
out=$(run tag v0.2.0 2>&1) || fail "a matching stable tag passes: $out"
assert_contains "a stable tag prints its version" "$out" "version=0.2.0"
assert_contains "a stable tag is not a prerelease" "$out" "prerelease=false"

set_fixture 0.2.0-rc.1 "0.2.0-rc.1 - 2026-12-01"
out=$(run tag v0.2.0-rc.1 2>&1) || fail "a matching prerelease tag passes: $out"
assert_contains "a prerelease tag prints its version" "$out" "version=0.2.0-rc.1"
assert_contains "a prerelease tag is a prerelease" "$out" "prerelease=true"

for bad in 0.2.0 v0.2 v0.2.0+build.1 vx v0.2.0- 'v0.2.0-rc..1!' v0.2.0-rc- v01.2.3x; do
	set_fixture 0.2.0 "0.2.0 - 2026-12-01"
	assert_fails "the tag '$bad' is rejected" run tag "$bad"
done

set_fixture 0.2.1 "0.2.1 - 2026-12-01"
assert_fails "a manifest that differs from the tag fails" run tag v0.2.0
set_fixture 0.2.01 "0.2.01 - 2026-12-01"
assert_fails "a manifest 0.2.01 does not match tag v0.2.0" run tag v0.2.0

for word in unreleased Unreleased UNRELEASED; do
	set_fixture 0.2.0 "0.2.0 - $word"
	assert_fails "a heading that says $word fails" run tag v0.2.0
	assert_contains "the message for $word says unreleased" "$FAILED_OUTPUT" "unreleased"
done
set_fixture 0.2.0 "0.1.0 - 2026-11-01" "0.2.0 - 2026-12-01"
assert_fails "a first heading naming another version fails even if a later one is right" run tag v0.2.0
set_fixture 0.2.0 "0.2.01 - 2026-12-01"
assert_fails "a first heading 0.2.01 does not match 0.2.0" run tag v0.2.0
set_fixture 0.2.0 "0.2.0"
out=$(run tag v0.2.0 2>&1) || fail "a bare heading naming the version passes: $out"
pass "a bare heading naming the version passes"

set_fixture 0.3.0 "0.3.0 - 2026-12-02"
out=$(run dispatch ignored-argument 2>&1) || fail "dispatch passes: $out"
assert_contains "dispatch prints the manifest's version" "$out" "version=0.3.0"
set_fixture 0.3.0 "0.3.0 - unreleased"
assert_fails "dispatch fails on an unreleased heading" run dispatch
set_fixture 0.3.0-rc.1 "0.3.0-rc.1 - 2026-12-02"
out=$(run dispatch 2>&1) || fail "dispatch of a prerelease passes: $out"
assert_contains "dispatch marks a prerelease" "$out" "prerelease=true"

out=$(run version 1.2.3-rc.1 2>&1) || fail "version mode accepts a prerelease: $out"
assert_contains "version mode prints the prerelease flag" "$out" "prerelease=true"
out=$(run version 1.2.3 2>&1) || fail "version mode accepts a stable version: $out"
assert_contains "version mode marks a stable version" "$out" "prerelease=false"
assert_fails "version mode rejects build metadata" run version 1.2.3+x
assert_fails "version mode rejects a leading v" run version v1.2.3

set_fixture 0.2.0 "0.2.0 - 2026-12-01"
(cd "$repo" && git checkout --quiet -b side && git commit --allow-empty -m "feat: side" --quiet)
assert_fails "a tag commit that is not an ancestor of main fails" run tag v0.2.0
assert_contains "the ancestry message names main" "$FAILED_OUTPUT" "main"
(cd "$repo" && git checkout --quiet main)
out=$(run tag v0.2.0 2>&1) || fail "a tag commit on main passes: $out"
pass "a tag commit on main passes"

echo "all verify-release assertions passed"
