#!/bin/sh
# Tests install.sh against a fake release, with stubs for uname, id, sudo and
# systemctl. Run from the repository root.
set -u
# shellcheck source=scripts/test/lib.sh
. "$(dirname "$0")/lib.sh"

script=$PWD/install.sh
assert_executable "install.sh exists and is executable" "$script"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

stubs=$tmp/stubs
release=$tmp/release
units=$tmp/units
mkdir -p "$stubs" "$release" "$units"

sha() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	else
		shasum -a 256 "$1" | cut -d' ' -f1
	fi
}

cat >"$stubs/uname" <<'STUB'
#!/bin/sh
echo "${FAKE_ARCH:-x86_64}"
STUB
cat >"$stubs/id" <<'STUB'
#!/bin/sh
case $1 in
-u) echo "${FAKE_UID:-1000}" ;;
-un) echo tester ;;
esac
STUB
cat >"$stubs/sudo" <<'STUB'
#!/bin/sh
exec "$@"
STUB
cat >"$stubs/systemctl" <<STUB
#!/bin/sh
echo "\$*" >>"$tmp/systemctl.log"
case \$1 in
is-active) [ -f "$tmp/active" ]; exit \$? ;;
stop) rm -f "$tmp/active" ;;
restart) touch "$tmp/active" ;;
esac
exit 0
STUB
chmod +x "$stubs"/*

# make_release <version>: a fake release of a binary, a page tarball and the sums.
make_release() {
	_v=$1
	_d=$release/v$_v
	mkdir -p "$_d"
	cat >"$_d/prusa-connect-moonraker_${_v}_linux_amd64" <<BIN
#!/bin/sh
if [ "\$1" = "--version" ]; then
	echo "prusa-connect $_v"
	exit 0
fi
echo "\$PWD \$*" >>"$tmp/module.log"
BIN
	chmod +x "$_d/prusa-connect-moonraker_${_v}_linux_amd64"
	printf 'bundle %s\n' "$_v" >"$_d/moonraker-module_$_v.tar.gz"
	{
		printf '%s  prusa-connect-moonraker_%s_linux_amd64\n' "$(sha "$_d/prusa-connect-moonraker_${_v}_linux_amd64")" "$_v"
		printf '%s  moonraker-module_%s.tar.gz\n' "$(sha "$_d/moonraker-module_$_v.tar.gz")" "$_v"
	} >"$_d/SHA256SUMS"
}

# install <home> <script> [args...]: runs the script, leaving $code and $out.
install() {
	_home=$1
	_script=$2
	shift 2
	out=$(env PATH="$stubs:$PATH" HOME="$_home" \
		PRUSA_CONNECT_DOWNLOAD_BASE="file://$release" PRUSA_CONNECT_UNIT_DIR="$units" \
		sh "$_script" "$@" 2>&1)
	code=$?
}

make_release 0.1.0
make_release 0.2.0
home=$tmp/home
dir=$home/prusa-connect

# A fresh install. The v in the version is optional.
install "$home" "$script" --version v0.1.0
assert_eq "a fresh install succeeds" 0 "$code"
assert_executable "the binary is installed" "$dir/prusa-connect"
assert_contains "the binary is the requested version" "$("$dir/prusa-connect" --version)" "0.1.0"
config=$(cat "$dir/config/config.yaml")
assert_contains "the config declares moonraker" "$config" "type: moonraker"
assert_contains "the config declares xbuddy" "$config" "type: xbuddy"
assert_contains "the config turns upgrades off" "$config" "enabled: false"
unit=$(cat "$units/prusa-connect.service")
assert_contains "the unit runs as the user" "$unit" "User=tester"
assert_contains "the unit runs in the install directory" "$unit" "WorkingDirectory=$dir"
assert_contains "the unit starts the binary" "$unit" "ExecStart=$dir/prusa-connect"
module_call=$(cat "$tmp/module.log")
assert_contains "the page is installed from the install directory" "$module_call" "$dir module add"
assert_contains "the page is installed at the version" "$module_call" "github.com/mjonuschat/prusa-driver-moonraker@0.1.0"
assert_contains "the page is installed from a tarball" "$module_call" "-from "
assert_contains "the page is installed from the release tarball" "$module_call" "moonraker-module_0.1.0.tar.gz"
assert_contains "the page is installed with the starter config" "$module_call" "-config config/config.yaml"
assert_contains "the page is approved" "$module_call" "-approve"
calls=$(cat "$tmp/systemctl.log")
assert_not_contains "a fresh install stops nothing" "$calls" "stop"
assert_contains "the unit is reloaded" "$calls" "daemon-reload"
assert_contains "the service is enabled" "$calls" "enable prusa-connect"
assert_contains "the service is restarted" "$calls" "restart prusa-connect"
assert_contains "the address is printed" "$out" ":8080"
assert_contains "the setup code hint is printed" "$out" "setup_code"

# A second run, from a copy stamped the way the release stamps it, gives the same
# result and leaves the user's config alone.
sed "s/^DEFAULT_VERSION=.*/DEFAULT_VERSION=0.1.0/" "$script" >"$tmp/install-stamped.sh"
printf '# mine\n' >>"$dir/config/config.yaml"
install "$home" "$tmp/install-stamped.sh"
assert_eq "a second run succeeds with the stamped version" 0 "$code"
assert_contains "the config is untouched by the second run" "$(cat "$dir/config/config.yaml")" "# mine"
assert_eq "the page is installed again" 2 "$(wc -l <"$tmp/module.log" | tr -d ' ')"
assert_contains "the binary is still the requested version" "$("$dir/prusa-connect" --version)" "0.1.0"

# --version overrides the stamp of a stamped copy.
home6=$tmp/home6
install "$home6" "$tmp/install-stamped.sh" --version 0.2.0
assert_eq "a stamped copy accepts --version" 0 "$code"
assert_contains "--version wins over the stamp" "$("$home6/prusa-connect/prusa-connect" --version)" "0.2.0"

# An upgrade stops the running service first.
touch "$tmp/active"
: >"$tmp/systemctl.log"
install "$home" "$script" --version 0.2.0
assert_eq "an upgrade succeeds" 0 "$code"
assert_contains "the upgraded binary is the new version" "$("$dir/prusa-connect" --version)" "0.2.0"
assert_contains "the page is installed at the new version" "$(tail -n 1 "$tmp/module.log")" "@0.2.0"
calls=$(cat "$tmp/systemctl.log")
assert_contains "the running service is stopped" "$calls" "stop prusa-connect"
stop_line=$(grep -n '^stop' "$tmp/systemctl.log" | head -n 1 | cut -d: -f1)
restart_line=$(grep -n '^restart' "$tmp/systemctl.log" | head -n 1 | cut -d: -f1)
[ "$stop_line" -lt "$restart_line" ] || fail "the service is stopped before it is restarted"
pass "the service is stopped before it is restarted"

# A download that does not match the sums installs nothing.
make_release 0.3.0
printf 'tampered\n' >>"$release/v0.3.0/prusa-connect-moonraker_0.3.0_linux_amd64"
home2=$tmp/home2
install "$home2" "$script" --version 0.3.0
assert_eq "a checksum mismatch fails" 1 "$code"
assert_contains "the failure names the checksum" "$out" "checksum"
[ ! -e "$home2/prusa-connect" ] || fail "a checksum mismatch installs nothing"
pass "a checksum mismatch installs nothing"

# An existing config is left alone.
home3=$tmp/home3
mkdir -p "$home3/prusa-connect/config"
printf 'core:\n  api:\n    addr: ":9999"\n' >"$home3/prusa-connect/config/config.yaml"
install "$home3" "$script" --version 0.1.0
assert_eq "an install over an existing config succeeds" 0 "$code"
assert_contains "the existing config is kept" "$(cat "$home3/prusa-connect/config/config.yaml")" ':9999'
assert_not_contains "the starter config is not added to it" "$(cat "$home3/prusa-connect/config/config.yaml")" "xbuddy"

# Root is refused before anything is downloaded.
home4=$tmp/home4
out=$(env PATH="$stubs:$PATH" HOME="$home4" FAKE_UID=0 \
	PRUSA_CONNECT_DOWNLOAD_BASE="file://$tmp/nowhere" PRUSA_CONNECT_UNIT_DIR="$units" \
	sh "$script" --version 0.1.0 2>&1)
code=$?
assert_eq "root is refused" 1 "$code"
assert_contains "the refusal says root" "$out" "root"
[ ! -e "$home4/prusa-connect" ] || fail "root installs nothing"
pass "root installs nothing"

# The repository copy has no stamped version.
install "$tmp/home5" "$script"
assert_eq "no version fails" 1 "$code"
assert_contains "the message names --version" "$out" "--version"

echo "all install assertions passed"
