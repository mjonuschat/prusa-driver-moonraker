#!/bin/sh
# Decides whether a `svelte-check --output machine` run of module/ui passed.
#
# usage: svelte-check-filter.sh <log> <svelte-check exit status>
#
# svelte-check also reports type errors inside the core's own package sources,
# which this module links and does not own. Those are the only errors accepted:
# the ones whose file is under ../../prusa-connect/packages/. The run fails when
#   - the log is missing or empty, or has no COMPLETED line (the tool crashed),
#   - any error is reported for another file, whatever form its path takes, or
#   - the tool exited non-zero and reported no error at all (a configuration or
#     tool failure).
set -eu

if [ $# -ne 2 ]; then
	echo "usage: $0 <log> <svelte-check exit status>" >&2
	exit 2
fi
log=$1
status=$2

if [ ! -s "$log" ]; then
	echo "svelte-check-filter: $log is missing or empty" >&2
	exit 1
fi
if ! grep -q ' COMPLETED ' "$log"; then
	echo "svelte-check-filter: svelte-check did not finish; the log has no COMPLETED line" >&2
	exit 1
fi

errors=$(grep -E '^[0-9]+ ERROR ' "$log" || true)
own=$(printf '%s\n' "$errors" | grep -v '^$' | grep -Fv ' ERROR "../../prusa-connect/packages/' || true)
if [ -n "$own" ]; then
	echo "svelte-check-filter: errors outside the core's package sources:" >&2
	printf '%s\n' "$own" >&2
	exit 1
fi

if [ "$status" -ne 0 ] && [ -z "$errors" ]; then
	echo "svelte-check-filter: svelte-check exited $status and reported no error" >&2
	exit 1
fi
echo "svelte-check-filter: no error in this module's own sources"
