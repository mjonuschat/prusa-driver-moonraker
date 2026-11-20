#!/bin/sh
# Tests docker/config.yaml and docker/compose.yaml.
#
# usage: config.test.sh <binary> <bundle-dir>
#
# <binary> is a composed server built without the placeholder web app, and
# <bundle-dir> a built module/ui/dist. The static assertions need neither. The
# smoke assertions start the server on the host with a copy of the shipped
# config and check, through the API, that the moonraker connector is registered
# and that the module is offered from the bundle.
set -u
# shellcheck source=scripts/test/lib.sh
. "$(dirname "$0")/lib.sh"

config=docker/config.yaml
compose=docker/compose.yaml
module_id=github.com/mjonuschat/prusa-driver-moonraker

assert_file "the shipped config exists" "$config"
assert_file "the compose file exists" "$compose"

config_text=$(cat "$config")
assert_contains "the config declares the moonraker connector" "$config_text" "type: moonraker"
assert_contains "the config declares the xbuddy connector" "$config_text" "type: xbuddy"
assert_contains "the config offers the module from /opt/modules/moonraker" "$config_text" "$module_id: /opt/modules/moonraker"
upgrade_block=$(awk '/^  upgrade:/ { p = 1; next } p && /^  [a-z]/ { exit } p { print }' "$config")
assert_contains "upgrades are off" "$upgrade_block" "enabled: false"

compose_text=$(cat "$compose")
assert_contains "the compose file runs read-only" "$compose_text" "read_only: true"
assert_contains "the compose file drops every capability" "$compose_text" "cap_drop"
assert_contains "the compose file mounts the released config name" "$compose_text" "./config.docker.yaml:/config/config.yaml"

if [ $# -ne 2 ]; then
	echo "usage: $0 <binary> <bundle-dir>" >&2
	exit 2
fi
binary=$1
bundle=$2
[ -x "$binary" ] || fail "the binary $binary exists and is executable"
[ -f "$bundle/module.yaml" ] && [ -f "$bundle/ui/index.js" ] || fail "the bundle $bundle holds module.yaml and ui/index.js"

tmp=$(mktemp -d)
server_pid=""
cleanup() {
	if [ -n "$server_pid" ]; then
		kill "$server_pid" 2>/dev/null
		wait "$server_pid" 2>/dev/null
	fi
	rm -rf "$tmp"
}
trap cleanup EXIT

port=$((20000 + $$ % 20000))
case $bundle in
/*) ;;
*) bundle=$PWD/$bundle ;;
esac
case $binary in
/*) ;;
*) binary=$PWD/$binary ;;
esac

awk -v port="$port" -v bundle="$bundle" -v id="$module_id" '
	/^    addr:/ { print "    addr: \"127.0.0.1:" port "\""; next }
	/^    github.com\// {
		if (index($0, id ":") == 5) { print "    " id ": " bundle }
		next
	}
	{ print }
' "$config" >"$tmp/config.yaml"

(cd "$tmp" && "$binary" -config "$tmp/config.yaml" >"$tmp/server.log" 2>&1) &
server_pid=$!

ready=0
i=0
while [ $i -lt 60 ]; do
	if curl -fsS "http://127.0.0.1:$port/api/v1/health" >/dev/null 2>&1; then
		ready=1
		break
	fi
	kill -0 "$server_pid" 2>/dev/null || break
	i=$((i + 1))
	sleep 1
done
if [ "$ready" -ne 1 ]; then
	tail -n 20 "$tmp/server.log" >&2
	fail "server did not become ready within 60 seconds"
fi
pass "the server becomes ready with the shipped config"

code=$(first_run_code "$(cat "$tmp/server.log")")
[ -n "$code" ] || fail "the server logged a first-run setup code"
complete_setup "http://127.0.0.1:$port" "$code" || fail "first-run setup completes"
pass "first-run setup completes"

connectors=$(curl -fsS "http://127.0.0.1:$port/api/v1/vocabularies/extensions" | jq -r '.items[] | select(.kind == "connector") | .name')
assert_contains "the moonraker connector is registered" "$connectors" "moonraker"

offer=$(curl -fsS "http://127.0.0.1:$port/api/v1/modules/index" | jq -r --arg id "$module_id" '.items[] | select(.module == $id) | .source')
assert_eq "the module is offered from the bundle" "$bundle" "$offer"

echo "all config assertions passed"
