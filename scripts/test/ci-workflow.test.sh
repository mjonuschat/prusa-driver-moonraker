#!/bin/sh
# Tests the structure of .github/workflows/ci.yml with the job helpers in lib.sh.
set -u
# shellcheck source=scripts/test/lib.sh
. "$(dirname "$0")/lib.sh"

wf=.github/workflows/ci.yml
assert_file "ci.yml exists" "$wf"

text=$(cat "$wf")
assert_contains "ci.yml runs on pull requests" "$text" "pull_request:"
assert_contains "ci.yml runs on a schedule" "$text" "schedule:"
assert_contains "ci.yml can be called by the release" "$text" "workflow_call:"

unpinned=$(grep -E '^\s*(- )?uses:' "$wf" | grep -v 'uses: \./' | grep -Ev '@[0-9a-f]{40} # v[0-9]' || true)
assert_eq "every action is pinned to a SHA with a version comment" "" "$unpinned"

checkouts=$(grep -c 'actions/checkout@' "$wf")
no_persist=$(grep -c 'persist-credentials: false' "$wf")
assert_eq "every checkout sets persist-credentials: false" "$checkouts" "$no_persist"

concurrency=$(awk '/^concurrency:/ { p = 1; print; next } p && /^[a-z]/ { exit } p { print }' "$wf")
assert_contains "the concurrency group includes the run id" "$concurrency" "github.run_id"
assert_contains "only pull requests cancel in progress" "$concurrency" "cancel-in-progress: \${{ github.event_name == 'pull_request' }}"

assert_contains "the default permissions are contents: read" "$text" "contents: read"

assert_contains "web-app uploads ci-web-app" "$(job_block "$wf" web-app)" "name: ci-web-app"
assert_contains "web-app asserts index.html" "$(job_block "$wf" web-app)" "index.html"
assert_contains "ui uploads ci-ui-dist" "$(job_block "$wf" ui)" "name: ci-ui-dist"
assert_contains "ui judges svelte-check with the filter script" "$(job_block "$wf" ui)" "scripts/svelte-check-filter.sh"
assert_not_contains "ui no longer filters with a bare grep" "$(job_block "$wf" ui)" "grep -E"

build_needs=$(job_needs "$wf" build | tr '\n' ' ')
assert_contains "build needs web-app" "$build_needs" "web-app"
assert_contains "build needs ui" "$build_needs" "ui"
build=$(job_block "$wf" build)
assert_contains "build downloads ci-web-app" "$build" "name: ci-web-app"
assert_contains "build downloads ci-ui-dist" "$build" "name: ci-ui-dist"
assert_contains "build runs the composed build script" "$build" "scripts/build-server.sh"
assert_not_contains "build never opts out of the web app check" "$build" "--allow-placeholder"
assert_contains "build runs the config smoke test" "$build" "scripts/test/config.test.sh"

image=$(job_block "$wf" image)
assert_contains "image builds docker/Dockerfile" "$image" "docker/Dockerfile"
assert_contains "image runs the nonroot check" "$image" "check-container-nonroot.sh"
assert_contains "image runs the probes check" "$image" "check-container-probes.sh"
assert_contains "image runs the privileged port check" "$image" "check-container-privileged-port.sh"
assert_contains "image runs the image smoke test" "$image" "scripts/test/image.test.sh"
assert_not_contains "image never pushes" "$image" "push"

assert_contains "scripts runs the script tests" "$(job_block "$wf" scripts)" "make test-scripts"
assert_contains "scripts installs a pinned git-cliff and checks its checksum" "$(job_block "$wf" scripts)" "sha256sum -c -"
assert_contains "dockerfile-sync runs the drift check" "$(job_block "$wf" dockerfile-sync)" "scripts/check-dockerfile-sync.sh"
assert_contains "vuln runs govulncheck" "$(job_block "$wf" vuln)" "govulncheck"
assert_contains "workflows runs actionlint" "$(job_block "$wf" workflows)" "actionlint"
assert_contains "test runs the race detector" "$(job_block "$wf" test)" "go test -race ./..."
assert_contains "lint runs golangci-lint" "$(job_block "$wf" lint)" "golangci-lint"

# No job may skip, so the jobs carry no condition that could skip them.
for job in web-app lint test ui build vuln dockerfile-sync workflows scripts image; do
	assert_eq "$job has no job-level if" "" "$(job_if "$wf" "$job")"
done

assert_eq "ci-ok runs always" "always()" "$(job_if "$wf" ci-ok)"
ok_block=$(job_block "$wf" ci-ok)
assert_contains "ci-ok runs check-needs.sh" "$ok_block" "scripts/check-needs.sh"
assert_contains "ci-ok passes the needs context as JSON" "$ok_block" "toJSON(needs)"
ok_needs=$(job_needs "$wf" ci-ok | tr '\n' ' ')
for job in web-app lint test ui build vuln dockerfile-sync workflows scripts image; do
	assert_contains "ci-ok needs $job" "$ok_needs" "$job"
done

echo "all ci-workflow assertions passed"
