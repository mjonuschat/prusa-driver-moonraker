#!/bin/sh
# Verify the local installer resolves the checkout beside a relocated module.
set -u
# shellcheck source=scripts/test/lib.sh
. "$(dirname "$0")/lib.sh"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
project="$tmp/relocated project"
stubs=$tmp/stubs
mkdir -p "$project/module/ui" "$project/prusa-connect/packages/svelte/dist/build" "$stubs"
touch "$project/prusa-connect/packages/svelte/dist/build/module.js"
touch "$project/prusa-connect/go.mod"
# Keep an old hardcoded installer from touching its original checkout.
sed 's|^CHECKOUT=/Users/.*|CHECKOUT=/nonexistent-module-install-test|' module/install.sh > "$project/module/install.sh"
cat > "$stubs/pnpm" <<'STUB'
#!/bin/sh
if [ "$1" = run ] && [ "$2" = build ]; then
	mkdir -p dist/ui
	touch dist/module.yaml dist/ui/index.js
fi
STUB
cat > "$stubs/go" <<'STUB'
#!/bin/sh
printf '%s\n' "$*" > "$MODULE_TEST_LOG"
STUB
chmod +x "$stubs/pnpm" "$stubs/go"

output=$(env PATH="$stubs:$PATH" MODULE_TEST_LOG="$tmp/go.log" sh "$project/module/install.sh" 2>&1)
status=$?
assert_eq "the relocated module installs" 0 "$status"
assert_contains "the installer uses its neighboring checkout" "$(cat "$tmp/go.log")" "-C $project/prusa-connect "
assert_contains "the installer reports its relocated work directory" "$output" "$project/prusa-connect/bin/machines-demo"
