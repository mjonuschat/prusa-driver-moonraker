#!/bin/sh
# Tests the structure of .github/workflows/release.yml with the job helpers in lib.sh.
# The ${{ }} expressions are GitHub Actions syntax, matched as text on purpose.
# shellcheck disable=SC2016
set -u
# shellcheck source=scripts/test/lib.sh
. "$(dirname "$0")/lib.sh"

wf=.github/workflows/release.yml
assert_file "release.yml exists" "$wf"

text=$(cat "$wf")
assert_contains "the tag trigger is the glob v*" "$text" "- 'v*'"
assert_contains "the release can be dispatched by hand" "$text" "workflow_dispatch:"
assert_workflow_hygiene "$wf"

# The jobs and their needs, as in the spec.
assert_eq "verify has no needs" "" "$(job_needs "$wf" verify)"
for job in ci web-app module; do
	assert_contains "$job needs verify" "$(job_needs "$wf" "$job")" "verify"
done
for job in dist publish image; do
	assert_contains "$job needs verify" "$(job_needs "$wf" "$job")" "verify"
done
assert_contains "dist needs web-app" "$(job_needs "$wf" dist)" "web-app"
for job in ci dist module; do
	assert_contains "publish needs $job" "$(job_needs "$wf" publish)" "$job"
done
assert_contains "image needs publish" "$(job_needs "$wf" image)" "publish"

for job in publish image; do
	if is_tag_push_gate "$(job_if "$wf" "$job")"; then
		pass "$job runs only for a push of a tag"
	else
		fail "$job runs only for a push of a tag: its job-level if is '$(job_if "$wf" "$job")'"
	fi
done
# The jobs that only build run on a dispatch too, so they carry no such condition.
for job in verify ci web-app dist module; do
	assert_eq "$job has no publication condition" "" "$(job_if "$wf" "$job")"
done

verify=$(job_block "$wf" verify)
assert_contains "verify runs verify-release.sh" "$verify" "scripts/verify-release.sh"
assert_contains "verify uses tag mode for a push and dispatch mode otherwise" "$verify" "github.event_name == 'push' && 'tag' || 'dispatch'"
assert_contains "verify takes the tag name from the environment" "$verify" "github.ref_name"
assert_contains "verify checks out the whole history" "$verify" "fetch-depth: 0"
assert_contains "verify outputs the version" "$verify" "version:"
assert_contains "verify outputs the prerelease flag" "$verify" "prerelease:"

assert_contains "ci calls ci.yml" "$(job_block "$wf" ci)" "uses: ./.github/workflows/ci.yml"

webapp=$(job_block "$wf" web-app)
assert_contains "web-app uploads web-app" "$webapp" "name: web-app"
assert_not_contains "web-app restores no cache" "$webapp" "actions/cache"

dist=$(job_block "$wf" dist)
for arch in amd64 arm64 armv7; do
	assert_contains "dist builds $arch" "$dist" "$arch"
done
assert_contains "dist downloads web-app" "$dist" "name: web-app"
assert_contains "dist runs the build script" "$dist" "scripts/build-server.sh"
assert_not_contains "dist never opts out of the web app check" "$dist" "--allow-placeholder"
assert_contains "dist takes the version from verify" "$dist" "needs.verify.outputs.version"
assert_contains "dist uploads one artifact per target" "$dist" "name: server-"

module=$(job_block "$wf" module)
assert_contains "module uploads the module artifact" "$module" "name: module"
assert_contains "module packs moonraker-module_<version>.tar.gz" "$module" "moonraker-module_"
# module add refuses an archive that holds a "./" entry, so the bundle is packed
# by the names of its entries and never as ".".
dot_pack='-czf "out/moonraker-module_${VERSION}.tar.gz" .'
assert_not_contains "module does not pack the bundle as a dot" "$module" "$dot_pack"
assert_contains "module packs from inside the bundle directory" "$module" "cd module/ui/dist && tar -czf"
assert_contains "module packs the entries by a wildcard after --" "$module" '.tar.gz" -- *)'

publish=$(job_block "$wf" publish)
assert_contains "publish downloads the server artifacts" "$publish" "pattern: server-*"
assert_contains "publish downloads the module artifact" "$publish" "name: module"
assert_not_contains "publish never downloads web-app" "$publish" "name: web-app"
assert_not_contains "publish never downloads ci-web-app" "$publish" "ci-web-app"
assert_contains "publish checks out the whole history" "$publish" "fetch-depth: 0"
assert_contains "publish has contents: write" "$publish" "contents: write"
assert_contains "publish writes SHA256SUMS" "$publish" "SHA256SUMS"

assert_contains "publish attaches the config as config.docker.yaml" "$publish" "config.docker.yaml"
assert_contains "publish attaches the compose file as compose.yaml" "$publish" "compose.yaml"
assert_contains "publish verifies the tag exists" "$publish" "--verify-tag"
assert_contains "publish marks a prerelease from verify" "$publish" "needs.verify.outputs.prerelease"
assert_contains "publish adds the prerelease flag" "$publish" "--prerelease"

gh_regex='(^|[^A-Za-z0-9_-])gh [a-z]'
for job in verify publish image; do
	assert_eq "every gh step of $job sets GH_TOKEN from github.token" "" \
		"$(steps_missing "$wf" "$job" "$gh_regex" 'GH_TOKEN: ${{ github.token }}')"
done

assert_contains "the compose bind source is the released config name" "$(cat docker/compose.yaml)" "./config.docker.yaml:/config/config.yaml"

image=$(job_block "$wf" image)
assert_contains "image calls container-image.yml" "$image" "uses: ./.github/workflows/container-image.yml"
assert_contains "image passes the version from verify" "$image" "needs.verify.outputs.version"
for permission in "contents: read" "packages: write" "id-token: write"; do
	assert_contains "image grants $permission" "$image" "$permission"
done

concurrency=$(awk '/^concurrency:/ { p = 1; print; next } p && /^[a-z]/ { exit } p { print }' "$wf")
assert_contains "the release concurrency group is its own" "$concurrency" "release-"
assert_contains "a release is never cancelled" "$concurrency" "cancel-in-progress: false"

echo "all release-workflow assertions passed"
