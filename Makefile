.PHONY: test build sim update-core

test:
	go test ./...

build:
	cd prusa-connect && $(MAKE) compose-build BUILD_FILE=../build.yaml

sim:
	go run ./driver/cmd/moonraker-sim

update-core:
	git submodule update --remote prusa-connect
