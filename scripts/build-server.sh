#!/bin/sh
# usage: build-server.sh [--allow-placeholder] <goos> <goarch> <goarm> <version> <out>
#
# <version> is this driver's version. The binary reports it after the core's, as
# <core version>+moonraker-<version>, where the core version is the first line
# of prusa-connect/VERSION. The documentation link names the core version alone,
# because the core's docs are not versioned by this driver.
#
# compose-build runs `go run` to detect build tags, which fails under a
# cross-compiling GOARCH. This script runs compose-build's own steps instead and
# applies the cross environment only to the last one: generate the module,
# `go mod tidy`, detect the tags natively, then `go build`.
#
# Env:
#   EXTRA_TAGS  extra build tags; default "observability postgres", which with the
#               detected tags is the set the core's own release builds with
#   DOCS_URL    the documentation link template stamped into the binary, with
#               {version} replaced by the core version; unset uses the core's
#               default template
#   APPDIST     where the core's web app is embedded from
#
# The build refuses to run unless $APPDIST/index.html exists, because a server
# built over the placeholder compiles and serves no page. --allow-placeholder
# lifts that and is for this script's own tests only.
set -eu

usage() {
	echo "usage: $0 [--allow-placeholder] <goos> <goarch> <goarm> <version> <out>" >&2
	exit 2
}

allow_placeholder=0
if [ "${1:-}" = "--allow-placeholder" ]; then
	allow_placeholder=1
	shift
fi
[ $# -eq 5 ] || usage

goos=$1
goarch=$2
goarm=$3
version=$4
out=$5
[ -n "$version" ] || usage

root=$(CDPATH="" cd -- "$(dirname "$0")/.." && pwd)
core=$root/prusa-connect
appdist=${APPDIST:-$core/internal/transport/http/httpapi/appdist}
extra_tags=${EXTRA_TAGS-observability postgres}

if [ "$allow_placeholder" -eq 0 ] && [ ! -f "$appdist/index.html" ]; then
	echo "build-server: refusing to build: $appdist/index.html is missing, so the server would serve no web page. Build the web app first (make embedded-app)." >&2
	exit 1
fi

case $out in
/*) ;;
*) out=$PWD/$out ;;
esac
mkdir -p "$(dirname "$out")"

# An absolute scratch directory, because the steps below change directory.
tmpdir=${TMPDIR:-/tmp}
case $tmpdir in
/*) ;;
*) tmpdir=$PWD/$tmpdir ;;
esac
# Exported, so the go commands below, which run in other directories, see it too.
TMPDIR=$tmpdir
export TMPDIR
work=$(mktemp -d "$tmpdir/build-server.XXXXXX")
trap 'rm -rf "$work"' EXIT

# native <command...>: runs a command for the machine this script runs on. A
# caller may have exported a target such as GOARCH, and a `go run` that inherited
# it would build a binary it cannot execute.
native() {
	env -u GOOS -u GOARCH -u GOARM -u CGO_ENABLED "$@"
}

# 1. Generate the composed module natively. compose-build prints to stdout, and
#    this script's stdout is its contract, so that goes to stderr.
(cd "$core" && native go run ./cmd/compose-build --config "$root/build.yaml" --print --work "$work") >&2

# 2. Resolve its dependencies.
(cd "$work" && native go mod tidy) >&2

# 3. Detect the tags natively, as compose-build does.
detected=$(cd "$work" && native go run github.com/prusa3d/prusa-connect/cmd/detect-build-tags \
	--config "$core/config/config.yaml" \
	--adapters "$core/adapters/tags.json" \
	--variant full \
	--extra "$extra_tags")
tags="full $detected"
tags=$(printf '%s' "$tags" | tr -s ' ' | sed 's/ $//')

core_version=$(tr -d '[:space:]' <"$core/VERSION")
[ -n "$core_version" ] || {
	echo "build-server: $core/VERSION is empty" >&2
	exit 1
}
stamp="$core_version+moonraker-$version"

default_docs="https://beta.prusa3d.com/connect-local/docs/{version}/"
docs_template=${DOCS_URL:-$default_docs}
docs_url=$(printf '%s' "$docs_template" | sed "s|{version}|$core_version|g")

ldflags="-X github.com/prusa3d/prusa-connect/internal/kernel/version.stamped=$stamp"
ldflags="$ldflags -X github.com/prusa3d/prusa-connect/internal/kernel/version.docsURL=$docs_url"

# 4. Cross-compile. The target environment is set on this command only.
(cd "$work" && CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" GOARM="$goarm" \
	go build -tags "$tags" -ldflags "$ldflags" -o "$out" .) >&2

echo "tags: $tags"
echo "wrote: $out"
