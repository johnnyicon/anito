VERSION ?= $(shell git describe --tags --exact-match 2>/dev/null || echo "dev")
BINARY  := ~/.local/bin/anito
BUILD   := go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY) ./cmd/anito/
UI_DIR  := internal/server/ui

PLIST := $(HOME)/Library/LaunchAgents/com.anito.daemon.plist

.PHONY: build install reload start stop release test ui-build ui-dev

## ui-build: compile the React SPA into internal/server/ui/dist
ui-build:
	cd $(UI_DIR) && npm run build

## ui-dev: run the Vite dev server (proxies API to localhost:7700)
ui-dev:
	cd $(UI_DIR) && npm run dev

## build: compile the React SPA then the Go binary to ~/.local/bin/anito
build: ui-build
	$(BUILD)
	@echo "built anito $(VERSION) → $(BINARY)"

## install: alias for build
install: build

## reload: build + reload the running daemon (requires launchd agent installed)
reload: build
	anito reload

## start: load the launchd agent (starts the daemon)
start:
	launchctl load $(PLIST)
	@echo "anito started"

## stop: unload the launchd agent (stops the daemon)
stop:
	launchctl unload $(PLIST)
	@echo "anito stopped"

## release VERSION=v1.2.3: tag + build
release:
	@if [ -z "$(filter-out $@,$(MAKECMDGOALS))" ] && [ "$(VERSION)" = "dev" ]; then \
		echo "usage: make release VERSION=v1.2.3"; exit 1; fi
	git tag $(VERSION)
	$(BUILD)
	@echo "tagged $(VERSION) and built"

## test: run all tests
test:
	go test ./...

## help: list targets
help:
	@grep -E '^## ' Makefile | sed 's/## //'
