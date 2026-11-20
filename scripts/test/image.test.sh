#!/bin/sh
# Tests the built image with the shipped config, the way compose runs it.
#
# usage: image.test.sh <image>
#
# It starts <image> with docker/config.yaml mounted at /config/config.yaml, a
# read-only root and no capabilities, then checks through the API that the
# server is healthy and that the Moonraker module is offered from
# /opt/modules/moonraker inside the image.
set -u
# shellcheck source=scripts/test/lib.sh
. "$(dirname "$0")/lib.sh"

config=docker/config.yaml
module_id=github.com/mjonuschat/prusa-driver-moonraker

assert_file "the shipped config exists" "$config"

if [ $# -ne 1 ]; then
	echo "usage: $0 <image>" >&2
	exit 2
fi
image=$1

command -v docker >/dev/null 2>&1 || fail "docker is available"
docker image inspect "$image" >/dev/null 2>&1 || fail "the image $image exists"

suffix=$$
name=moonraker-image-test-$suffix
volumes="$name-config $name-storage $name-data $name-files"
cleanup() {
	docker rm -f "$name" >/dev/null 2>&1
	for volume in $volumes; do
		docker volume rm -f "$volume" >/dev/null 2>&1
	done
}
trap cleanup EXIT

port=$((20000 + suffix % 20000))
config_abs=$PWD/$config

docker run -d --name "$name" \
	--read-only --cap-drop ALL --security-opt no-new-privileges:true \
	-p "127.0.0.1:$port:8080" \
	-v "$config_abs:/config/config.yaml:ro" \
	-v "$name-config:/config/config.d" \
	-v "$name-storage:/storage" \
	-v "$name-data:/data" \
	-v "$name-files:/files" \
	--tmpfs /tmp \
	"$image" >/dev/null || fail "the container starts"
pass "the container starts"

ready=0
i=0
while [ $i -lt 90 ]; do
	if curl -fsS "http://127.0.0.1:$port/api/v1/health" >/dev/null 2>&1; then
		ready=1
		break
	fi
	[ "$(docker inspect -f '{{.State.Running}}' "$name" 2>/dev/null)" = "true" ] || break
	i=$((i + 1))
	sleep 1
done
if [ "$ready" -ne 1 ]; then
	docker logs --tail 30 "$name" >&2
	fail "the server did not become ready within 90 seconds"
fi
pass "the server is healthy with the shipped config"

code=$(first_run_code "$(docker logs "$name" 2>&1)")
[ -n "$code" ] || fail "the server logged a first-run setup code"
complete_setup "http://127.0.0.1:$port" "$code" || fail "first-run setup completes"
pass "first-run setup completes"

assert_eq "the image runs as the unprivileged user" "prusa" "$(docker inspect -f '{{.Config.User}}' "$name")"

connectors=$(curl -fsS "http://127.0.0.1:$port/api/v1/vocabularies/extensions" | jq -r '.items[] | select(.kind == "connector") | .name')
assert_contains "the moonraker connector is registered in the image" "$connectors" "moonraker"

offer=$(curl -fsS "http://127.0.0.1:$port/api/v1/modules/index" | jq -r --arg id "$module_id" '.items[] | select(.module == $id) | .source')
assert_eq "the module is offered from /opt/modules/moonraker" "/opt/modules/moonraker" "$offer"

echo "all image assertions passed"
