#!/bin/sh
# Tests the structure of .github/workflows/container-image.yml.
# The ${{ }} expressions are GitHub Actions syntax, matched as text on purpose.
# shellcheck disable=SC2016
set -u
# shellcheck source=scripts/test/lib.sh
. "$(dirname "$0")/lib.sh"

wf=.github/workflows/container-image.yml
assert_file "container-image.yml exists" "$wf"

assert_workflow_hygiene "$wf"

call_inputs=$(awk '/^  workflow_call:/ { p = 1; next } p && /^  workflow_dispatch:/ { exit } p { print }' "$wf")
dispatch_inputs=$(awk '/^  workflow_dispatch:/ { p = 1; next } p && /^[a-z]/ { exit } p { print }' "$wf")
for section in "$call_inputs" "$dispatch_inputs"; do
	assert_contains "an entry point takes the version" "$section" "version:"
	assert_contains "an entry point takes move-tags" "$section" "move-tags:"
done
move_default() {
	printf '%s\n' "$1" | awk '/move-tags:/ { p = 1; next } p && /default:/ { print $2; exit }'
}
assert_eq "a call from the release moves the floating tags" "true" "$(move_default "$call_inputs")"
assert_eq "a manual dispatch moves no floating tags by default" "false" "$(move_default "$dispatch_inputs")"

publish=$(job_block "$wf" publish)
assert_contains "the publishing job uses the release environment" "$publish" "environment: release"
assert_contains "the publishing job can write packages" "$publish" "packages: write"
assert_contains "the publishing job can sign with its OIDC token" "$publish" "id-token: write"

line_of() {
	printf '%s\n' "$publish" | grep -n -- "$1" | head -n 1 | cut -d: -f1
}
before() {
	[ -n "$2" ] && [ -n "$3" ] && [ "$2" -lt "$3" ] || fail "$1 (lines $2 and $3)"
	pass "$1"
}

trusted=$(line_of 'actions/checkout@')
validate=$(line_of 'verify-release.sh version')
release_view=$(line_of 'gh release view')
tag_checkout=$(line_of 'ref: v')
before "the trusted checkout comes before the validation" "$trusted" "$validate"
before "the validation comes before the release check" "$validate" "$release_view"
before "the release check comes before the checkout of the tag" "$release_view" "$tag_checkout"
assert_contains "the tag is checked out with its submodules" "$publish" "submodules: recursive"

for check in check-container-nonroot.sh check-container-probes.sh check-container-privileged-port.sh scripts/test/image.test.sh; do
	before "$check runs before the push" "$(line_of "$check")" "$(line_of 'push: true')"
done

meta=$(printf '%s\n' "$publish" | awk '/id: meta/ { p = 1 } p { print } p && /^$/ { exit }')
assert_contains "the exact tag is a raw tag" "$meta" "type=raw"
assert_not_contains "the push has no semver tags" "$meta" "type=semver"
assert_contains "the metadata turns the latest tag off" "$meta" "latest=false"
assert_contains "the image is pushed for amd64 and arm64" "$publish" "platforms: linux/amd64,linux/arm64"
assert_contains "the push has provenance" "$publish" "provenance: mode=max"
assert_contains "the push has an SBOM" "$publish" "sbom: true"
assert_contains "the push exposes its digest" "$publish" "steps.push.outputs.digest"

assert_contains "cosign signs the pushed digest" "$publish" 'cosign sign --yes "$IMAGE@$DIGEST"'
before "signing comes after the push" "$(line_of 'push: true')" "$(line_of 'cosign sign --yes')"

last_step=$(printf '%s\n' "$publish" | grep -E '^      - name:' | tail -n 1)
assert_contains "the last step moves the floating tags" "$last_step" "floating tags"
floating=$(printf '%s\n' "$publish" | awk '/name: Move the floating tags/ { p = 1 } p { print }')
assert_contains "the floating step runs only when move-tags is set" "$floating" "if: inputs.move-tags"
assert_contains "the floating step reads the tags through the API" "$floating" "gh api"
assert_contains "the floating step uses floating-tags.sh" "$floating" "scripts/floating-tags.sh"
assert_not_contains "the floating step needs no git credentials" "$floating" "git fetch"
before "the floating step comes after signing" "$(line_of 'cosign sign --yes')" "$(line_of 'Move the floating tags')"

assert_eq "every gh step sets GH_TOKEN from github.token" "" \
	"$(steps_missing "$wf" publish '(^|[^A-Za-z0-9_-])gh [a-z]' 'GH_TOKEN: ${{ github.token }}')"

echo "all image-workflow assertions passed"
