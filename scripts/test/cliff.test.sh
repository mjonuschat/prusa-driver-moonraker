#!/bin/sh
# Tests cliff.toml on a controlled temporary repository, so the result does not
# depend on this repository's history or on a shallow checkout.
set -u
# shellcheck source=scripts/test/lib.sh
. "$(dirname "$0")/lib.sh"

assert_file "cliff.toml exists" cliff.toml

if ! command -v git-cliff >/dev/null 2>&1; then
	echo "skipped: git-cliff not installed"
	exit 0
fi

config=$PWD/cliff.toml
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

repo=$tmp/repo
git_temp_repo "$repo"
commit() {
	git -C "$repo" commit --allow-empty --quiet -m "$1"
}
commit "feat: add the first feature"
commit "fix: repair the first bug"
git -C "$repo" tag v0.1.0
commit "feat: add a second feature"
commit "docs: describe the second feature"
commit "build: pin an action"
git -C "$repo" tag v0.2.0

notes=$(cd "$repo" && git cliff --config "$config" --latest --strip header 2>"$tmp/cliff.err") || fail "git-cliff runs on the temporary repository: $(cat "$tmp/cliff.err")"
assert_contains "the latest notes have a Features group" "$notes" "### Features"
assert_contains "the latest notes list the new feature" "$notes" "Add a second feature"
assert_contains "the latest notes have a Documentation group" "$notes" "### Documentation"
assert_contains "the latest notes have a Build group" "$notes" "### Build"
assert_not_contains "the latest notes leave out the earlier release's commits" "$notes" "Add the first feature"

full=$(cd "$repo" && git cliff --config "$config" 2>"$tmp/cliff.err") || fail "git-cliff renders the whole changelog: $(cat "$tmp/cliff.err")"
assert_contains "the changelog has a Bug Fixes group" "$full" "### Bug Fixes"
assert_contains "the changelog links the comparison between releases" "$full" "https://github.com/mjonuschat/prusa-driver-moonraker/compare/v0.1.0...v0.2.0"
assert_not_contains "the changelog has no aldis link" "$full" "aldis"

if printf '%s\n' "$full" | LC_ALL=C grep -q '[^[:print:][:space:]]'; then
	fail "the changelog holds only ASCII text"
fi
pass "the changelog holds only ASCII text"

echo "all cliff assertions passed"
