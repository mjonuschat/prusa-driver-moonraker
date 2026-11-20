# shellcheck shell=sh
# Shared helpers for scripts/test/*.test.sh. Source it: . "$(dirname "$0")/lib.sh"
# A test exits non-zero on its first failed assertion and prints its name.

fail() {
	printf 'FAIL: %s\n' "$1" >&2
	exit 1
}

pass() {
	printf 'ok: %s\n' "$1"
}

assert_executable() {
	[ -x "$2" ] || fail "$1: $2 is missing or not executable"
	pass "$1"
}

assert_file() {
	[ -f "$2" ] || fail "$1: $2 is missing"
	pass "$1"
}

assert_eq() {
	[ "$2" = "$3" ] || fail "$1: expected '$2', got '$3'"
	pass "$1"
}

assert_contains() {
	case $2 in
	*"$3"*) pass "$1" ;;
	*) fail "$1: expected output to contain '$3', got '$2'" ;;
	esac
}

assert_not_contains() {
	case $2 in
	*"$3"*) fail "$1: expected output not to contain '$3', got '$2'" ;;
	*) pass "$1" ;;
	esac
}

# assert_fails <name> <command...>
# The command must exit non-zero. Its combined output is left in $FAILED_OUTPUT.
assert_fails() {
	_name=$1
	shift
	if FAILED_OUTPUT=$("$@" 2>&1); then
		fail "$_name: expected a non-zero exit, got 0 (output: $FAILED_OUTPUT)"
	fi
	pass "$_name"
}

# git_temp_repo <dir>
# Makes a repository with a test-only identity, so a test that commits works on
# a clean account or runner.
git_temp_repo() {
	mkdir -p "$1"
	git -C "$1" init --quiet
	git -C "$1" config user.name "Test"
	git -C "$1" config user.email "test@example.invalid"
	git -C "$1" config commit.gpgsign false
	git -C "$1" config tag.gpgsign false
}

# job_block <workflow-file> <job>
# Prints the lines of one job of a workflow, from "  <job>:" to the next
# two-space key.
job_block() {
	awk -v job="$2" '
		$0 ~ "^  " job ":[ ]*$" { inside = 1; print; next }
		inside && /^  [A-Za-z0-9_-]+:/ { exit }
		inside { print }
	' "$1"
}

# job_if <workflow-file> <job>
# Prints the job-level "if:" value: the "if:" at four-space indent directly
# under the job key. An "if:" inside a step is deeper and is never printed.
job_if() {
	job_block "$1" "$2" | awk '/^    if:/ { sub(/^    if:[ ]*/, ""); print; exit }'
}

# job_needs <workflow-file> <job>
# Prints the job-level needs, one job name per line, from either
# "needs: a", "needs: [a, b]" or a block list.
job_needs() {
	job_block "$1" "$2" | awk '
		/^    needs:/ {
			line = $0
			sub(/^    needs:[ ]*/, "", line)
			if (line != "") {
				gsub(/[\[\],]/, " ", line)
				n = split(line, parts, " ")
				for (i = 1; i <= n; i++) if (parts[i] != "") print parts[i]
				exit
			}
			inlist = 1
			next
		}
		inlist && /^      - / { sub(/^      - /, ""); print; next }
		inlist { exit }
	'
}

# first_run_code <server log text>
# Prints the one-time setup code a server logs while it has not been set up.
first_run_code() {
	printf '%s\n' "$1" | sed -n 's/.*setup_code=\([A-Za-z0-9-]*\).*/\1/p' | head -n 1
}

# complete_setup <base url> <setup code>
# Finishes a server's first run with open access, so its API answers. A server
# that has not been set up serves nothing else.
complete_setup() {
	curl -fsS -X POST "$1/api/v1/setup" -H 'Content-Type: application/json' \
		-d "{\"code\":\"$2\",\"mode\":\"open\"}" >/dev/null
}

# is_tag_push_gate <condition>
# Succeeds only when the condition is a push event AND a tag ref, with no OR.
is_tag_push_gate() {
	case $1 in
	*"||"*) return 1 ;;
	*"github.event_name == 'push'"*"&&"*"github.ref_type == 'tag'"*) return 0 ;;
	*) return 1 ;;
	esac
}

# steps_missing <workflow-file> <job> <command-regex> <required-text>
# Prints the name of every step of the job whose text matches the regex but does
# not contain the required text. Steps start at six-space indent with "- ".
steps_missing() {
	job_block "$1" "$2" | awk -v re="$3" -v need="$4" '
		function flush() { if (inside && step ~ re && index(step, need) == 0) print name }
		/^      - / { flush(); inside = 1; step = $0; name = $0; sub(/^      - (name: )?/, "", name); next }
		inside { step = step "\n" $0 }
		END { flush() }
	'
}

# assert_workflow_hygiene <workflow-file>
# Every action is pinned to a commit SHA with its version in a comment, and every
# checkout turns off persisted credentials.
assert_workflow_hygiene() {
	_unpinned=$(grep -E '^\s*(- )?uses:' "$1" | grep -v 'uses: \./' | grep -Ev '@[0-9a-f]{40} # v[0-9]' || true)
	assert_eq "$1: every action is pinned to a SHA with a version comment" "" "$_unpinned"
	_checkouts=$(grep -c 'actions/checkout@' "$1")
	_no_persist=$(grep -c 'persist-credentials: false' "$1")
	assert_eq "$1: every checkout sets persist-credentials: false" "$_checkouts" "$_no_persist"
}
