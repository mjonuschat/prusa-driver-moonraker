#!/bin/sh
# Tests the workflow helpers in scripts/test/lib.sh with fixtures. A gate test
# that asserts "a && b" on a job's own if must see a weakened condition as wrong.
# The ${{ }} expressions are GitHub Actions syntax, matched as text on purpose.
# shellcheck disable=SC2016
set -u
# shellcheck source=scripts/test/lib.sh
. "$(dirname "$0")/lib.sh"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

good=$tmp/good.yml
cat >"$good" <<'YML'
name: fixture
jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - run: echo verify
  publish:
    needs: [verify, ci]
    if: github.event_name == 'push' && github.ref_type == 'tag'
    runs-on: ubuntu-latest
    steps:
      - run: echo publish
  image:
    needs:
      - verify
      - publish
    if: github.event_name == 'push' && github.ref_type == 'tag'
    steps:
      - run: echo image
YML

in_step=$tmp/in-step.yml
cat >"$in_step" <<'YML'
jobs:
  publish:
    needs: verify
    runs-on: ubuntu-latest
    steps:
      - name: guard
        if: github.event_name == 'push' && github.ref_type == 'tag'
        run: echo publish
YML

with_or=$tmp/with-or.yml
cat >"$with_or" <<'YML'
jobs:
  publish:
    if: github.event_name == 'push' || github.ref_type == 'tag'
    steps:
      - run: echo publish
YML

absent=$tmp/absent.yml
cat >"$absent" <<'YML'
jobs:
  publish:
    steps:
      - run: echo publish
YML

block=$(job_block "$good" publish)
assert_contains "job_block holds the job's own lines" "$block" "echo publish"
assert_not_contains "job_block stops at the next job" "$block" "echo image"
assert_not_contains "job_block does not start earlier" "$block" "echo verify"

assert_eq "job_if reads the job-level condition" \
	"github.event_name == 'push' && github.ref_type == 'tag'" "$(job_if "$good" publish)"
assert_eq "job_if ignores an if inside a step" "" "$(job_if "$in_step" publish)"
assert_eq "job_if is empty when there is no if" "" "$(job_if "$absent" publish)"
assert_eq "job_if sees an OR condition as it is" \
	"github.event_name == 'push' || github.ref_type == 'tag'" "$(job_if "$with_or" publish)"

gate_ok() {
	is_tag_push_gate "$(job_if "$1" publish)"
}
if gate_ok "$good"; then pass "the gate check accepts the real condition"; else fail "the gate check accepts the real condition"; fi
if gate_ok "$in_step"; then fail "the gate check rejects a condition moved into a step"; else pass "the gate check rejects a condition moved into a step"; fi
if gate_ok "$with_or"; then fail "the gate check rejects an OR condition"; else pass "the gate check rejects an OR condition"; fi
if gate_ok "$absent"; then fail "the gate check rejects a missing condition"; else pass "the gate check rejects a missing condition"; fi

tokens=$tmp/tokens.yml
cat >"$tokens" <<'YML'
jobs:
  good:
    steps:
      - name: Create the release
        env:
          GH_TOKEN: ${{ github.token }}
        run: gh release create v1
      - name: Build
        run: make build
  empty:
    steps:
      - name: Create the release
        env:
          GH_TOKEN:
        run: gh release create v1
  other:
    steps:
      - name: Create the release
        env:
          GH_TOKEN: ${{ secrets.OTHER }}
        run: gh release create v1
  absent:
    steps:
      - name: View the release
        run: gh release view v1
YML
need='GH_TOKEN: ${{ github.token }}'
gh_regex='(^|[^A-Za-z0-9_-])gh [a-z]'
assert_eq "steps_missing accepts a step with the right token" "" "$(steps_missing "$tokens" good "$gh_regex" "$need")"
assert_eq "steps_missing flags an empty token" "Create the release" "$(steps_missing "$tokens" empty "$gh_regex" "$need")"
assert_eq "steps_missing flags another expression" "Create the release" "$(steps_missing "$tokens" other "$gh_regex" "$need")"
assert_eq "steps_missing flags an absent token" "View the release" "$(steps_missing "$tokens" absent "$gh_regex" "$need")"

assert_eq "job_needs reads an inline list" "verify
ci" "$(job_needs "$good" publish)"
assert_eq "job_needs reads a block list" "verify
publish" "$(job_needs "$good" image)"
assert_eq "job_needs reads a single value" "verify" "$(job_needs "$in_step" publish)"
assert_eq "job_needs is empty for a job with none" "" "$(job_needs "$good" verify)"

log='time=2026-11-20T07:16:00.732-08:00 level=WARN msg="this server has recorded nothing" setup="POST /api/v1/setup" setup_code=EXZV-H09A-NC1Q'
assert_eq "first_run_code reads the logged code" "EXZV-H09A-NC1Q" "$(first_run_code "$log")"
assert_eq "first_run_code is empty when there is none" "" "$(first_run_code "level=INFO msg=ready")"

repo=$tmp/repo
git_temp_repo "$repo"
if (cd "$repo" && git commit --allow-empty -m "feat: x" --quiet); then
	pass "git_temp_repo can commit"
else
	fail "git_temp_repo can commit"
fi

echo "all lib assertions passed"
