.PHONY: test lint build embedded-app sim update-core test-scripts test-smoke

test:
	go test -race ./...

lint:
	golangci-lint run ./...

# The core embeds its web app from this directory, which is gitignored: a clean
# or a submodule update empties it, and compose-build does not rebuild it. A
# binary composed over an empty directory answers 404 at /.
APP_INDEX := prusa-connect/internal/transport/http/httpapi/appdist/index.html

build: embedded-app
	cd prusa-connect && $(MAKE) compose-build BUILD_FILE=../build.yaml

# Builds the web app only when it is missing, because it takes minutes. To
# rebuild it anyway, run `make -C prusa-connect embed-app`.
embedded-app:
	@if [ ! -f "$(APP_INDEX)" ]; then \
		echo "The web app is not built, so building it for the binary to embed..."; \
		$(MAKE) -C prusa-connect embed-app; \
	fi

sim:
	go run ./driver/cmd/moonraker-sim

update-core:
	git submodule update --remote prusa-connect

# The tools the script tests need. Missing ones are named before anything runs.
SCRIPT_TOOLS := go git jq shellcheck actionlint awk file

# The behavioural tests of everything in scripts/, and shellcheck over them. The
# smoke tests need a built server, bundle or image, so test-smoke runs those.
test-scripts:
	@missing=""; for tool in $(SCRIPT_TOOLS); do \
		command -v $$tool >/dev/null 2>&1 || missing="$$missing $$tool"; \
	done; \
	if [ -n "$$missing" ]; then echo "test-scripts: missing tools:$$missing" >&2; exit 1; fi
	@set -e; for t in scripts/test/*.test.sh; do \
		case $$t in */config.test.sh|*/image.test.sh) continue ;; esac; \
		echo "== $$t"; sh $$t; \
	done
	shellcheck -x install.sh scripts/*.sh scripts/test/*.sh

# The smoke tests. BINARY is a composed server, BUNDLE a built module/ui/dist,
# IMAGE an image name. The image test runs only when IMAGE is set, and with
# IMAGE set and no working docker it fails when REQUIRE_DOCKER=1 and skips
# otherwise.
test-smoke:
	@if [ -z "$(BINARY)" ] || [ -z "$(BUNDLE)" ]; then \
		echo "usage: make test-smoke BINARY=<server> BUNDLE=<module/ui/dist> [IMAGE=<image>] [REQUIRE_DOCKER=1]" >&2; exit 2; \
	fi
	sh scripts/test/config.test.sh "$(BINARY)" "$(BUNDLE)"
	@if [ -n "$(IMAGE)" ]; then \
		if docker info >/dev/null 2>&1; then sh scripts/test/image.test.sh "$(IMAGE)"; \
		elif [ "$(REQUIRE_DOCKER)" = "1" ]; then echo "test-smoke: docker is required and not available" >&2; exit 1; \
		else echo "skipped: docker not available"; fi; \
	fi
