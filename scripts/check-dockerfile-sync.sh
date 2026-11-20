#!/bin/sh
# docker/Dockerfile is a maintained copy of the core's docker/server/Dockerfile.
# One of its first three lines records the SHA-256 of the core's file it was copied
# from. A parser directive such as "# syntax=" has to stay on the first line, so
# the header may sit on the second:
#
#   # upstream-sha256: <hex>
#
# Two checks, reported apart:
#   1. the core's file still hashes to the recorded value. A mismatch means the
#      core changed, and the copy must be re-synced.
#   2. the runtime stage, everything from the last FROM to the end, is the core's
#      exactly, except for one added line that copies this module's bundle to
#      /opt/modules/moonraker. The stages before it may differ freely, because
#      they build from a different layout. The config is mounted, not copied.
#
# Env: CORE_DOCKERFILE (default prusa-connect/docker/server/Dockerfile) and
# OUR_DOCKERFILE (default docker/Dockerfile).
#
# Exit codes: 0 in sync, 1 the core changed, 2 the runtime stage differs,
# 3 the hash header is missing, 4 a file is missing.
set -eu

core=${CORE_DOCKERFILE:-prusa-connect/docker/server/Dockerfile}
ours=${OUR_DOCKERFILE:-docker/Dockerfile}

for file in "$core" "$ours"; do
	if [ ! -f "$file" ]; then
		echo "check-dockerfile-sync: file missing: $file" >&2
		exit 4
	fi
done

sha() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	else
		shasum -a 256 "$1" | cut -d' ' -f1
	fi
}

recorded=$(head -n 3 "$ours" | sed -n 's/^# upstream-sha256:[[:space:]]*\([0-9a-f]\{64\}\)[[:space:]]*$/\1/p' | head -n 1)
if [ -z "$recorded" ]; then
	echo "check-dockerfile-sync: header missing: one of the first three lines of $ours must be '# upstream-sha256: <hex>'" >&2
	exit 3
fi

current=$(sha "$core")
if [ "$recorded" != "$current" ]; then
	echo "check-dockerfile-sync: core changed: $core hashes to $current but $ours records $recorded. Re-sync $ours with the core's file and update its header." >&2
	exit 1
fi

runtime_stage() {
	awk '/^FROM / { n = 0 } { lines[++n] = $0 } END { for (i = 1; i <= n; i++) print lines[i] }' "$1"
}

stage_dir=$(mktemp -d "${TMPDIR:-/tmp}/dockerfile-sync.XXXXXX")
trap 'rm -rf "$stage_dir"' EXIT
runtime_stage "$core" >"$stage_dir/core"
runtime_stage "$ours" >"$stage_dir/ours"

# Anything removed or changed from the core's lines, and anything added beyond
# one module COPY, is drift.
changes=$(diff "$stage_dir/core" "$stage_dir/ours" || true)
removed=$(printf '%s\n' "$changes" | grep -c '^< ' || true)
added=$(printf '%s\n' "$changes" | grep '^> ' || true)
added_count=0
[ -n "$added" ] && added_count=$(printf '%s\n' "$added" | wc -l | tr -d ' ')

if [ "$removed" -ne 0 ] || [ "$added_count" -ne 1 ] ||
	! printf '%s\n' "$added" | grep -Eq '^> COPY .* /opt/modules/moonraker/?[[:space:]]*$'; then
	echo "check-dockerfile-sync: runtime stage differs from the core's beyond one added 'COPY ... /opt/modules/moonraker' line:" >&2
	printf '%s\n' "$changes" >&2
	exit 2
fi

echo "check-dockerfile-sync: in sync"
