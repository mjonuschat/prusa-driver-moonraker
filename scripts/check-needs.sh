#!/bin/sh
# The ci-ok aggregation. Reads the `needs` context as JSON from NEEDS_JSON and
# exits 0 only when every job's result is "success". A failed, cancelled or
# skipped job fails the check, and the message names it. Empty, invalid or
# job-less input fails too, because nothing was checked.
set -eu

: "${NEEDS_JSON:=}"
if [ -z "$NEEDS_JSON" ]; then
	echo "check-needs: NEEDS_JSON is empty" >&2
	exit 1
fi

if ! printf '%s' "$NEEDS_JSON" | jq -e 'type == "object" and length > 0' >/dev/null 2>&1; then
	echo "check-needs: NEEDS_JSON is not an object with at least one job" >&2
	exit 1
fi

bad=$(printf '%s' "$NEEDS_JSON" | jq -r 'to_entries[] | select(.value.result != "success") | "\(.key): \(.value.result)"')
if [ -n "$bad" ]; then
	echo "check-needs: these jobs did not succeed:" >&2
	printf '%s\n' "$bad" >&2
	exit 1
fi
echo "check-needs: every needed job succeeded"
