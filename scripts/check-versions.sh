#!/bin/sh
# usage: check-versions.sh
#
# Fails unless every file that names this driver's version names the same one:
# `version:` in module/module.yaml and "version" in module/ui/package.json. The
# release tag and the top changelog heading are checked by verify-release.sh,
# which calls this script.
#
# Env overrides, for tests:
#   MODULE_YAML   default module/module.yaml
#   PACKAGE_JSON  default module/ui/package.json
set -eu

module_yaml=${MODULE_YAML:-module/module.yaml}
package_json=${PACKAGE_JSON:-module/ui/package.json}

[ -f "$module_yaml" ] || {
	echo "check-versions: $module_yaml is missing" >&2
	exit 1
}
[ -f "$package_json" ] || {
	echo "check-versions: $package_json is missing" >&2
	exit 1
}

manifest=$(sed -n 's/^version:[[:space:]]*//p' "$module_yaml" | head -n 1 | sed 's/[[:space:]]*$//; s/^"//; s/"$//')
package=$(jq -r '.version // empty' "$package_json")

[ -n "$manifest" ] || {
	echo "check-versions: $module_yaml has no version" >&2
	exit 1
}
[ "$manifest" = "$package" ] || {
	echo "check-versions: $module_yaml says $manifest but $package_json says ${package:-nothing}" >&2
	exit 1
}

echo "version=$manifest"
