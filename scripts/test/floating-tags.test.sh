#!/bin/sh
# Tests scripts/floating-tags.sh. Run from the repository root.
set -u
# shellcheck source=scripts/test/lib.sh
. "$(dirname "$0")/lib.sh"

script=$PWD/scripts/floating-tags.sh
assert_executable "floating-tags.sh exists and is executable" "$script"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# tags_for <TAGS> <version>: the floating tags, joined with spaces.
# The script's status is checked before its output is reshaped, so a script that
# fails with no output cannot pass an assertion that expects no output.
tags_for() {
	_out=$(TAGS=$1 "$script" "$2") || {
		echo "SCRIPT-FAILED"
		return 0
	}
	printf '%s\n' "$_out" | tr '\n' ' ' | sed 's/ $//'
}

base="v0.1.0 v0.1.1 v0.2.0 v1.0.0"
assert_eq "1.0.0 moves 1.0, 1 and latest" "1.0 1 latest" "$(tags_for "$base" 1.0.0)"
assert_eq "0.2.0 moves 0.2 and 0, the highest 0.*" "0.2 0" "$(tags_for "$base" 0.2.0)"
assert_eq "0.1.0 moves nothing, 0.1.1 is higher" "" "$(tags_for "$base" 0.1.0)"
assert_eq "0.1.1 moves 0.1" "0.1" "$(tags_for "$base" 0.1.1)"

multi="v0.9.0 v0.10.0"
assert_eq "0.10.0 moves 0.10, 0 and latest, numerically" "0.10 0 latest" "$(tags_for "$multi" 0.10.0)"
assert_eq "0.9.0 moves 0.9 only, 0.10.0 is higher in 0" "0.9" "$(tags_for "$multi" 0.9.0)"

assert_eq "a prerelease moves nothing" "" "$(tags_for "v1.0.0-rc.1" 2.0.0-rc.1)"
assert_eq "prerelease tags in the set are ignored" "1.5 1 latest" "$(tags_for "v1.4.0 v2.0.0-rc.1" 1.5.0)"
assert_eq "an empty TAGS counts the version itself" "0.1 0 latest" "$(tags_for "" 0.1.0)"
assert_eq "garbage tags in the set are ignored" "1.0 1 latest" "$(tags_for "foo v1.0 v1.0.0+x nightly" 1.0.0)"

assert_fails "a version with a leading v is a usage error" env TAGS="" "$script" v1.0.0
assert_fails "no argument is a usage error" env TAGS="" "$script"

repo=$tmp/repo
git_temp_repo "$repo"
(cd "$repo" && git commit --allow-empty -m "feat: a" --quiet && git tag v0.1.0 && git tag v0.2.0)
repo_out=$(cd "$repo" && env -u TAGS "$script" 0.2.0) || fail "unset TAGS: the script exits 0"
repo_tags=$(printf '%s\n' "$repo_out" | tr '\n' ' ' | sed 's/ $//')
assert_eq "unset TAGS reads git tags" "0.2 0 latest" "$repo_tags"

echo "all floating-tags assertions passed"
