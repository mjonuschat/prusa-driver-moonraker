.PHONY: test lint build embedded-app sim update-core

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
