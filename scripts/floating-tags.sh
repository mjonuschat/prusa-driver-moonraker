#!/bin/sh
# usage: floating-tags.sh <version>        the version without a leading v
#
# The floating tags are X.Y, X and latest. A floating tag moves only for a
# stable release, and only when that release is the highest stable version in its
# scope: the highest X.Y.* for X.Y, the highest X.* for X, and the highest of
# all for latest. A prerelease prints nothing.
#
# The existing releases come from the env var TAGS, space separated, when TAGS is
# set, even when it is set empty. Otherwise they come from `git tag --list 'v*'`.
# Tags that are not vMAJOR.MINOR.PATCH are ignored, and so is any prerelease. The
# version itself counts as released, so the answer is the same before and after
# its tag exists.
set -eu

usage() {
	echo "usage: $0 <version>" >&2
	exit 2
}

[ $# -eq 1 ] || usage
version=$1
printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$' || usage

case $version in
*-*) exit 0 ;;
esac

if [ "${TAGS+x}" = x ]; then
	tags=$TAGS
else
	tags=$(git tag --list 'v*')
fi

# Compare numerically per component, since 0.10 is higher than 0.9.
{
	for tag in $tags; do
		printf '%s\n' "$tag"
	done
	printf 'v%s\n' "$version"
} | awk -v ver="$version" '
	BEGIN {
		split(ver, v, ".")
		vm = v[1] + 0; vn = v[2] + 0; vp = v[3] + 0
		minor_best = 1; major_best = 1; all_best = 1
	}
	/^v[0-9]+\.[0-9]+\.[0-9]+$/ {
		t = substr($0, 2)
		split(t, c, ".")
		m = c[1] + 0; n = c[2] + 0; p = c[3] + 0
		higher = (m > vm) || (m == vm && n > vn) || (m == vm && n == vn && p > vp)
		if (higher) {
			all_best = 0
			if (m == vm) major_best = 0
			if (m == vm && n == vn) minor_best = 0
		}
	}
	END {
		if (minor_best) print vm "." vn
		if (major_best) print vm
		if (all_best) print "latest"
	}
'
