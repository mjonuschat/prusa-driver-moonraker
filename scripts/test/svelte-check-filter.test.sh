#!/bin/sh
set -u
# shellcheck source=scripts/test/lib.sh
. "$(dirname "$0")/lib.sh"

script=scripts/svelte-check-filter.sh
assert_executable "svelte-check-filter.sh exists and is executable" "$script"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

upstream='1795187995435 ERROR "../../prusa-connect/packages/svelte/src/primitives/notifications/notifications.svelte" 332:17 "Type is not assignable"'
own='1795187995435 ERROR "src/settings.svelte" 83:16 "Object literal may only specify known properties"'
absolute='1795187995435 ERROR "/home/runner/work/x/module/ui/src/settings.svelte" 10:1 "Something"'
elsewhere='1795187995435 ERROR "../../other/place.ts" 1:1 "Something"'
start='1795187995424 START "/home/runner/work/x/module/ui"'
warning='1795187995435 WARNING "src/a.svelte" 1:1 "unused"'

# log <status> <lines...>: writes a log, runs the script, leaves its status in $code.
check() {
	_status=$1
	shift
	printf '%s\n' "$@" >"$tmp/log"
	"$script" "$tmp/log" "$_status" >"$tmp/out" 2>&1
	code=$?
}

check 0 "$start" '1795187995500 COMPLETED 100 FILES 0 ERRORS 0 WARNINGS 0 FILES_WITH_PROBLEMS'
assert_eq "a clean run passes" 0 "$code"

check 1 "$start" "$upstream" "$warning" '1795187995500 COMPLETED 5388 FILES 1 ERRORS 1 WARNINGS 1 FILES_WITH_PROBLEMS'
assert_eq "errors only in the core's package sources pass" 0 "$code"

check 1 "$start" "$upstream" "$own" '1795187995500 COMPLETED 5388 FILES 2 ERRORS 0 WARNINGS 2 FILES_WITH_PROBLEMS'
assert_eq "an error in the module's own sources fails" 1 "$code"
assert_contains "the message names the own-source error" "$(cat "$tmp/out")" "src/settings.svelte"

check 1 "$start" "$absolute" '1795187995500 COMPLETED 100 FILES 1 ERRORS 0 WARNINGS 1 FILES_WITH_PROBLEMS'
assert_eq "an error reported with an absolute path fails" 1 "$code"

check 1 "$start" "$elsewhere" '1795187995500 COMPLETED 100 FILES 1 ERRORS 0 WARNINGS 1 FILES_WITH_PROBLEMS'
assert_eq "an error outside the accepted upstream path fails" 1 "$code"

check 1 "$start" "$upstream"
assert_eq "a log that never completed fails" 1 "$code"
assert_contains "the message says it did not finish" "$(cat "$tmp/out")" "did not finish"

check 2 "$start" '1795187995500 COMPLETED 0 FILES 0 ERRORS 0 WARNINGS 0 FILES_WITH_PROBLEMS'
assert_eq "a non-zero exit with no reported error fails" 1 "$code"

: >"$tmp/log"
"$script" "$tmp/log" 0 >"$tmp/out" 2>&1
assert_eq "an empty log fails" 1 "$?"
"$script" "$tmp/does-not-exist" 0 >"$tmp/out" 2>&1
assert_eq "a missing log fails" 1 "$?"

echo "all svelte-check-filter assertions passed"
