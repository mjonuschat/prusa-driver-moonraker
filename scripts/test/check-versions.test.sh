#!/bin/sh
# Tests scripts/check-versions.sh. Run from the repository root.
set -u
# shellcheck source=scripts/test/lib.sh
. "$(dirname "$0")/lib.sh"

script=$PWD/scripts/check-versions.sh
assert_executable "check-versions.sh exists and is executable" "$script"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
manifest=$tmp/module.yaml
package=$tmp/package.json

run() {
	MODULE_YAML=$manifest PACKAGE_JSON=$package "$script"
}

printf 'name: machines\nversion: 0.2.0-rc.1\n' >"$manifest"
printf '{"version":"0.2.0-rc.1"}\n' >"$package"
out=$(run 2>&1) || fail "matching files pass: $out"
assert_contains "it prints the version" "$out" "version=0.2.0-rc.1"

printf '{"version":"0.2.0"}\n' >"$package"
assert_fails "a page package that differs fails" run
assert_contains "the message names both files" "$FAILED_OUTPUT" "package.json"

printf '{"name":"page"}\n' >"$package"
assert_fails "a page package with no version fails" run

printf 'name: machines\n' >"$manifest"
printf '{"version":"0.2.0"}\n' >"$package"
assert_fails "a manifest with no version fails" run

printf 'name: machines\nversion: 0.2.0\n' >"$manifest"
rm "$package"
assert_fails "a missing page package fails" run

echo "all check-versions assertions passed"
