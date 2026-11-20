#!/bin/sh
# Tests scripts/check-dockerfile-sync.sh with fixtures. Run from the repository root.
set -u
# shellcheck source=scripts/test/lib.sh
. "$(dirname "$0")/lib.sh"

script=$PWD/scripts/check-dockerfile-sync.sh
assert_executable "check-dockerfile-sync.sh exists and is executable" "$script"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

sha() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	else
		shasum -a 256 "$1" | cut -d' ' -f1
	fi
}

core=$tmp/core.Dockerfile
cat >"$core" <<'DOCKER'
FROM node:24 AS build
RUN echo build the web app
FROM debian:trixie-slim
COPY --from=build /app/dist /opt/modules/other
RUN useradd prusa
USER prusa
ENTRYPOINT ["/prusa_connect"]
DOCKER

# make_ours <file> <runtime-extra-lines-file or empty>: a copy of the core file
# with the hash header, a different build stage, and optional extra runtime lines.
make_ours() {
	{
		printf '# upstream-sha256: %s\n' "$(sha "$core")"
		printf 'FROM node:24 AS build\nRUN echo a different build stage\nRUN echo and another line\n'
		printf 'FROM debian:trixie-slim\n'
		printf 'COPY --from=build /app/dist /opt/modules/other\n'
		[ -n "$2" ] && cat "$2"
		printf 'RUN useradd prusa\nUSER prusa\nENTRYPOINT ["/prusa_connect"]\n'
	} >"$1"
}

run() {
	CORE_DOCKERFILE=$core OUR_DOCKERFILE=$1 "$script"
}

# do_run <ours>: runs the script, leaving its exit code in $code and its output
# in $RUN_OUTPUT. It runs in the current shell so both survive.
do_run() {
	RUN_OUTPUT=$(run "$1" 2>&1)
	code=$?
}

one=$tmp/one-copy.txt
printf 'COPY --from=ui /app/module/ui/dist /opt/modules/moonraker\n' >"$one"
two=$tmp/two-copies.txt
printf 'COPY --from=ui /app/a /opt/modules/moonraker\nCOPY --from=ui /app/b /opt/modules/moonraker\n' >"$two"
run_line=$tmp/run-line.txt
printf 'RUN echo /opt/modules/moonraker\n' >"$run_line"
extra_run=$tmp/extra-run.txt
printf 'RUN echo hello\n' >"$extra_run"

ours=$tmp/ours.Dockerfile

make_ours "$ours" "$one"
do_run "$ours"
assert_eq "one added module COPY and a rewritten build stage pass" 0 "$code"

make_ours "$ours" ""
do_run "$ours"
assert_eq "zero added module COPY lines fail" 2 "$code"
assert_contains "zero added copies say runtime stage" "$RUN_OUTPUT" "runtime stage"

make_ours "$ours" "$two"
do_run "$ours"
assert_eq "two added module COPY lines fail" 2 "$code"

make_ours "$ours" "$run_line"
do_run "$ours"
assert_eq "a RUN line that mentions the path fails" 2 "$code"

make_ours "$ours" "$extra_run"
do_run "$ours"
assert_eq "an extra RUN line fails" 2 "$code"

make_ours "$ours" "$one"
grep -v '^USER prusa' "$ours" >"$tmp/removed.Dockerfile"
do_run "$tmp/removed.Dockerfile"
assert_eq "removing a core runtime line fails" 2 "$code"
sed 's|^USER prusa|USER root|' "$ours" >"$tmp/altered.Dockerfile"
do_run "$tmp/altered.Dockerfile"
assert_eq "altering a core runtime line fails" 2 "$code"
grep -v '^COPY --from=build /app/dist /opt/modules/other' "$ours" >"$tmp/dropped-copy.Dockerfile"
do_run "$tmp/dropped-copy.Dockerfile"
assert_eq "removing a core COPY fails" 2 "$code"

core_with=$tmp/core-with.Dockerfile
sed 's|/opt/modules/other|/opt/modules/moonraker|' "$core" >"$core_with"
added_copy='COPY --from=ui /app/module/ui/dist /opt/modules/moonraker'
run_with() {
	RUN_OUTPUT=$(CORE_DOCKERFILE=$core_with OUR_DOCKERFILE=$1 "$script" 2>&1)
	code=$?
}
{
	printf '# upstream-sha256: %s\n' "$(sha "$core_with")"
	printf 'FROM debian:trixie-slim\n'
	printf 'COPY --from=build /app/dist /opt/modules/moonraker\n%s\n' "$added_copy"
	printf 'RUN useradd prusa\nUSER prusa\nENTRYPOINT ["/prusa_connect"]\n'
} >"$tmp/ours-keeps-core-copy.Dockerfile"
run_with "$tmp/ours-keeps-core-copy.Dockerfile"
assert_eq "keeping the core's COPY to the module path and adding one passes" 0 "$code"
# Ours drops the core's COPY and adds its own: the added line is there, so the
# only reason to fail is the removed core line.
{
	printf '# upstream-sha256: %s\n' "$(sha "$core_with")"
	printf 'FROM debian:trixie-slim\n'
	printf '%s\n' "$added_copy"
	printf 'RUN useradd prusa\nUSER prusa\nENTRYPOINT ["/prusa_connect"]\n'
} >"$tmp/ours-without-core-copy.Dockerfile"
run_with "$tmp/ours-without-core-copy.Dockerfile"
assert_eq "removing a core COPY to the module path fails even with one added" 2 "$code"
assert_contains "the removed core line is reported" "$RUN_OUTPUT" "< COPY --from=build /app/dist /opt/modules/moonraker"

make_ours "$ours" "$one"
cp "$core" "$tmp/core.orig"
echo "RUN echo a new core line" >>"$core"
do_run "$ours"
assert_eq "a changed core file fails" 1 "$code"
assert_contains "a changed core file says core changed" "$RUN_OUTPUT" "core changed"
cp "$tmp/core.orig" "$core"

make_ours "$ours" "$one"
tail -n +2 "$ours" >"$tmp/no-header.Dockerfile"
do_run "$tmp/no-header.Dockerfile"
assert_eq "a missing header fails" 3 "$code"
assert_contains "a missing header says header" "$RUN_OUTPUT" "header"

do_run "$tmp/does-not-exist"
assert_eq "a missing local file fails" 4 "$code"
assert_contains "the missing local file is named" "$RUN_OUTPUT" "does-not-exist"
RUN_OUTPUT=$(CORE_DOCKERFILE=$tmp/no-core OUR_DOCKERFILE=$ours "$script" 2>&1)
assert_eq "a missing core file fails" 4 "$?"
assert_contains "the missing core file is named" "$RUN_OUTPUT" "no-core"

echo "all check-dockerfile-sync assertions passed"
