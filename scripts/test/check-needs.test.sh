#!/bin/sh
# Tests scripts/check-needs.sh. Run from the repository root.
set -u
# shellcheck source=scripts/test/lib.sh
. "$(dirname "$0")/lib.sh"

script=$PWD/scripts/check-needs.sh
assert_executable "check-needs.sh exists and is executable" "$script"

run() {
	NEEDS_JSON=$1 "$script"
}

ok='{"lint":{"result":"success","outputs":{}},"test":{"result":"success","outputs":{}}}'
if run "$ok" >/dev/null 2>&1; then
	pass "all successful jobs pass"
else
	fail "all successful jobs pass"
fi

for result in failure cancelled skipped; do
	json=$(printf '{"lint":{"result":"success"},"build":{"result":"%s"}}' "$result")
	assert_fails "a $result job fails the check" run "$json"
	assert_contains "the message names the $result job" "$FAILED_OUTPUT" "build"
	assert_contains "the message says $result" "$FAILED_OUTPUT" "$result"
done

assert_fails "empty NEEDS_JSON fails" run ""
assert_fails "invalid NEEDS_JSON fails" run "not json"
assert_fails "an empty needs object fails, nothing was checked" run "{}"

if env -u NEEDS_JSON "$script" >/dev/null 2>&1; then
	fail "an unset NEEDS_JSON fails"
else
	pass "an unset NEEDS_JSON fails"
fi

echo "all check-needs assertions passed"
