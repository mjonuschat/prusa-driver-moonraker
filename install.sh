#!/bin/sh
# Installs or upgrades the Prusa Connect server with the Moonraker driver and
# runs it as a systemd service. Running it again upgrades.
#
#   curl -fsSL https://github.com/mjonuschat/prusa-driver-moonraker/releases/latest/download/install.sh | sh
#   sh install.sh --version <version>
#
# The release stamps its version into the DEFAULT_VERSION line below. A copy that
# is not stamped needs --version, and --version overrides the stamp.
set -eu

DEFAULT_VERSION=
VERSION=$DEFAULT_VERSION

REPO=mjonuschat/prusa-driver-moonraker
BASE=${PRUSA_CONNECT_DOWNLOAD_BASE:-https://github.com/$REPO/releases/download}
UNIT_DIR=${PRUSA_CONNECT_UNIT_DIR:-/etc/systemd/system}

die() {
	echo "install.sh: $1" >&2
	exit 1
}

while [ $# -gt 0 ]; do
	case $1 in
	--version)
		[ $# -ge 2 ] || die "--version needs a value"
		VERSION=$2
		shift 2
		;;
	-h | --help)
		echo "usage: install.sh [--version <version>]"
		exit 0
		;;
	*) die "unknown argument: $1" ;;
	esac
done

VERSION=${VERSION#v}
[ -n "$VERSION" ] || die "no version to install: pass --version <version>"
[ "$(id -u)" -ne 0 ] || die "do not run this as root. It asks for sudo when it needs it."
for tool in curl sudo systemctl; do
	command -v "$tool" >/dev/null 2>&1 || die "$tool is needed and was not found"
done

case $(uname -m) in
x86_64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
armv7l) arch=armv7 ;;
*) die "unsupported architecture $(uname -m): this supports x86_64, aarch64 and armv7l" ;;
esac

binary=prusa-connect-moonraker_${VERSION}_linux_${arch}
page=moonraker-module_${VERSION}.tar.gz
dir=$HOME/prusa-connect

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

for file in "$binary" "$page" SHA256SUMS; do
	curl -fsSL -o "$tmp/$file" "$BASE/v$VERSION/$file" || die "could not download $file for version $VERSION"
done

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	else
		shasum -a 256 "$1" | cut -d' ' -f1
	fi
}

# verify <file>: the file matches exactly one line of SHA256SUMS.
verify() {
	expected=$(awk -v name="$1" '$2 == name { print $1 }' "$tmp/SHA256SUMS")
	[ -n "$expected" ] && [ "$(printf '%s\n' "$expected" | wc -l | tr -d ' ')" -eq 1 ] &&
		[ "$expected" = "$(sha256 "$tmp/$1")" ] ||
		die "checksum mismatch for $1. Nothing was installed."
}
verify "$binary"
verify "$page"

if systemctl is-active --quiet prusa-connect; then
	sudo systemctl stop prusa-connect
fi

mkdir -p "$dir/config"
cp "$tmp/$binary" "$dir/.prusa-connect.new"
chmod 755 "$dir/.prusa-connect.new"
mv -f "$dir/.prusa-connect.new" "$dir/prusa-connect"

if [ ! -e "$dir/config/config.yaml" ]; then
	cat >"$dir/config/config.yaml" <<'CONFIG'
core:
  connectors:
    moonraker:
      type: moonraker
      enabled: true
    xbuddy:
      type: xbuddy
      enabled: true
  # Back up before you upgrade. This is not an official Prusa release, so Prusa
  # Connect's built-in upgrade and its automatic backups are off. Run install.sh
  # again to upgrade.
  upgrade:
    enabled: false
CONFIG
fi

(cd "$dir" && ./prusa-connect module add "github.com/$REPO@$VERSION" -from "$tmp/$page" -config config/config.yaml -approve)

sudo tee "$UNIT_DIR/prusa-connect.service" >/dev/null <<UNIT
[Unit]
Description=Prusa Connect with the Moonraker driver
After=network-online.target
Wants=network-online.target

[Service]
User=$(id -un)
WorkingDirectory=$dir
ExecStart=$dir/prusa-connect
Restart=on-failure
NoNewPrivileges=yes

[Install]
WantedBy=multi-user.target
UNIT
sudo systemctl daemon-reload
sudo systemctl enable prusa-connect
sudo systemctl restart prusa-connect

echo "Prusa Connect $VERSION is installed and running at http://$(hostname):8080"
echo "On the first run it asks for a setup code. Find it with:"
echo "  sudo journalctl -u prusa-connect | grep setup_code"
echo "Run this command again to upgrade."
