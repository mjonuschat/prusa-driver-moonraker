#!/bin/sh
# Tests scripts/build-server.sh. Run from the repository root.
set -u
# shellcheck source=scripts/test/lib.sh
. "$(dirname "$0")/lib.sh"

script=scripts/build-server.sh
assert_executable "build-server.sh exists and is executable" "$script"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

empty_app=$tmp/empty-appdist
real_app=$tmp/real-appdist
mkdir -p "$empty_app" "$real_app"
printf '<html></html>' >"$real_app/index.html"

APPDIST=$empty_app assert_fails "an empty appdist is refused" "$script" linux amd64 "" 9.9.9-test "$tmp/refused"
assert_contains "the refusal names index.html" "$FAILED_OUTPUT" "index.html"
assert_eq "the refusal builds nothing" "" "$(ls "$tmp/refused" 2>/dev/null)"

if ! APPDIST=$real_app "$script" linux amd64 "" 9.9.9-test "$tmp/with-app" >"$tmp/with-app.out" 2>&1; then
	cat "$tmp/with-app.out" >&2
	fail "a build with an index.html in appdist succeeds"
fi
pass "a build with an index.html in appdist succeeds"

check_target() {
	_label=$1
	_goarch=$2
	_goarm=$3
	_cpu=$4
	_out=$tmp/$_label
	if ! "$script" --allow-placeholder linux "$_goarch" "$_goarm" 9.9.9-test "$_out" >"$tmp/$_label.out" 2>&1; then
		cat "$tmp/$_label.out" >&2
		fail "linux $_label builds"
	fi
	[ -s "$_out" ] || fail "linux $_label output is non-empty"
	_desc=$(file "$_out" | tr '[:upper:]' '[:lower:]')
	# $_cpu is a list of spellings separated by |; any one will do.
	_matched=0
	_old_ifs=$IFS
	IFS='|'
	for _spelling in $_cpu; do
		case $_desc in *"$_spelling"*) _matched=1 ;; esac
	done
	IFS=$_old_ifs
	[ "$_matched" -eq 1 ] || fail "linux $_label is $_cpu: file says '$_desc'"
	pass "linux $_label is $_cpu"
	assert_contains "linux $_label is statically linked" "$_desc" "statically linked"
}
check_target amd64 amd64 "" "x86-64"
check_target arm64 arm64 "" "aarch64|arm64"
check_target armv7 arm 7 "eabi5"

host_os=$(go env GOOS)
host_arch=$(go env GOARCH)
host_out=$tmp/host
"$script" --allow-placeholder "$host_os" "$host_arch" "" 9.9.9-test "$host_out" >"$tmp/host.out" 2>"$tmp/host.err" || {
	cat "$tmp/host.err" >&2
	fail "the host build succeeds"
}
assert_contains "the host binary reports the stamped version" "$("$host_out" --version 2>&1)" "9.9.9-test"
tags_line=$(grep '^tags:' "$tmp/host.out" | sed 's/^tags:[ ]*//' | tr -s ' ')
assert_eq "the tags are the core release set" "full diagnostics observability postgres" "$tags_line"
wrote_line=$(grep '^wrote:' "$tmp/host.out" | sed 's/^wrote:[ ]*//')
case $wrote_line in
/*) pass "the wrote line holds an absolute path" ;;
*) fail "the wrote line holds an absolute path: got '$wrote_line'" ;;
esac

EXTRA_TAGS="" "$script" --allow-placeholder "$host_os" "$host_arch" "" 9.9.9-test "$tmp/no-extra" >"$tmp/no-extra.out" 2>&1 || {
	cat "$tmp/no-extra.out" >&2
	fail "a build with no extra tags succeeds"
}
no_extra=$(grep '^tags:' "$tmp/no-extra.out")
assert_not_contains "EXTRA_TAGS empty drops observability" "$no_extra" "observability"
assert_not_contains "EXTRA_TAGS empty drops postgres" "$no_extra" "postgres"

DOCS_URL="https://example.test/{version}/" "$script" --allow-placeholder "$host_os" "$host_arch" "" 9.9.9-test "$tmp/docs" >"$tmp/docs.out" 2>&1 || {
	cat "$tmp/docs.out" >&2
	fail "a build with DOCS_URL succeeds"
}
if grep -aq "example.test" "$tmp/docs"; then
	pass "DOCS_URL is stamped into the binary"
else
	fail "DOCS_URL is stamped into the binary"
fi

# 6b. A cross-compiling environment inherited from the caller must not break the
# native steps. riscv64 is foreign to every host, so a `go run` that inherited it
# would fail to execute.
foreign_out=$tmp/foreign
if ! GOOS=linux GOARCH=riscv64 "$script" --allow-placeholder linux amd64 "" 9.9.9-test "$foreign_out" >"$tmp/foreign.out" 2>&1; then
	cat "$tmp/foreign.out" >&2
	fail "a build succeeds with a foreign GOOS and GOARCH in the environment"
fi
pass "a build succeeds with a foreign GOOS and GOARCH in the environment"
assert_contains "the foreign environment does not change the target" "$(file "$foreign_out" | tr '[:upper:]' '[:lower:]')" "x86-64"

rel_work=$tmp/rel-run
mkdir -p "$rel_work/rel-tmp"
script_abs=$PWD/$script
if ! (cd "$rel_work" && TMPDIR=rel-tmp "$script_abs" --allow-placeholder linux amd64 "" 9.9.9-test "$tmp/rel-out" >"$tmp/rel.out" 2>&1); then
	cat "$tmp/rel.out" >&2
	fail "a build succeeds with a relative TMPDIR"
fi
pass "a build succeeds with a relative TMPDIR"
assert_eq "a relative TMPDIR is left empty" "" "$(ls "$rel_work/rel-tmp")"
assert_eq "nothing is left in the core checkout" "" "$(git status --short prusa-connect | grep build-server || true)"

scratch=$tmp/scratch
mkdir -p "$scratch"
TMPDIR=$scratch assert_fails "a bad goarch fails" "$script" --allow-placeholder linux notanarch "" 9.9.9-test "$tmp/bad"
assert_eq "a failed build leaves no scratch directory" "" "$(ls "$scratch")"

echo "all build-server assertions passed"
