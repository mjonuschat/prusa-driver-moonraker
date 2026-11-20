#!/bin/sh
# usage: verify-release.sh tag <tag>      a tag push: every check
#        verify-release.sh dispatch        a manual run: manifest and changelog only
#        verify-release.sh version <ver>   syntax only, for the image workflow's input
#
# On success it prints "version=<version>" and "prerelease=true|false" and exits
# 0. On failure it prints a reason to stderr and exits 1. A usage error exits 2.
#
# A tag is vMAJOR.MINOR.PATCH, or that followed by a hyphen and a prerelease of
# letters, digits, dots and hyphens that has no empty segment and does not end
# in a hyphen. Build metadata (+) is rejected, because a Docker tag cannot carry
# it, and so a hyphen always means a prerelease.
#
# Env overrides, for tests:
#   MODULE_YAML  the manifest, default module/module.yaml
#   CHANGELOG    the changelog, default module/CHANGELOG.md
#   PACKAGE_JSON the page's package file, default module/ui/package.json
#   MAIN_REF     what the tagged commit must be an ancestor of, default origin/main
#   TAG_COMMIT   the tagged commit, default HEAD
set -eu

die() {
	echo "verify-release: $1" >&2
	exit 1
}

usage() {
	echo "usage: $0 tag <tag> | dispatch | version <version>" >&2
	exit 2
}

module_yaml=${MODULE_YAML:-module/module.yaml}
changelog=${CHANGELOG:-module/CHANGELOG.md}
main_ref=${MAIN_REF:-origin/main}
tag_commit=${TAG_COMMIT:-HEAD}

# check_files: every file that names the version names the same one.
check_files() {
	"$(dirname "$0")/check-versions.sh" >/dev/null || die "the version files disagree"
}

check_version() {
	printf '%s\n' "$1" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$' ||
		die "'$1' is not a release version: use MAJOR.MINOR.PATCH or add -PRERELEASE, with no build metadata"
	case $1 in
	*-) die "'$1' is not a release version: a prerelease cannot end in a hyphen" ;;
	esac
}

prerelease_of() {
	case $1 in
	*-*) echo true ;;
	*) echo false ;;
	esac
}

manifest_version() {
	[ -f "$module_yaml" ] || die "$module_yaml is missing"
	sed -n 's/^version:[[:space:]]*//p' "$module_yaml" | head -n 1 | sed 's/[[:space:]]*$//; s/^"//; s/"$//; s/^'"'"'//; s/'"'"'$//'
}

# check_changelog <version>: the first "## " heading names the version and does
# not say unreleased.
check_changelog() {
	[ -f "$changelog" ] || die "$changelog is missing"
	heading=$(grep -m 1 '^## ' "$changelog" | sed 's/^## //' || true)
	[ -n "$heading" ] || die "$changelog has no '## ' heading"
	if printf '%s\n' "$heading" | grep -qi 'unreleased'; then
		die "the top heading of $changelog says unreleased: date it before releasing ('## $1 - <date>')"
	fi
	case $heading in
	"$1" | "$1 "*) ;;
	*) die "the top heading of $changelog is '$heading', not '## $1'" ;;
	esac
}

[ $# -ge 1 ] || usage
mode=$1

case $mode in
tag)
	[ $# -eq 2 ] || usage
	tag=$2
	case $tag in
	v*) ;;
	*) die "the tag '$tag' does not start with v" ;;
	esac
	version=${tag#v}
	check_version "$version"
	manifest=$(manifest_version)
	[ "$manifest" = "$version" ] || die "the tag is $tag but $module_yaml says version $manifest"
	check_files
	check_changelog "$version"
	git merge-base --is-ancestor "$tag_commit" "$main_ref" ||
		die "the tagged commit is not on main ($main_ref)"
	;;
dispatch)
	version=$(manifest_version)
	[ -n "$version" ] || die "$module_yaml has no version"
	check_version "$version"
	check_files
	check_changelog "$version"
	;;
version)
	[ $# -eq 2 ] || usage
	version=$2
	check_version "$version"
	;;
*)
	usage
	;;
esac

echo "version=$version"
echo "prerelease=$(prerelease_of "$version")"
