VERSION ?= $(shell git describe --tags --exact-match 2>/dev/null || echo "dev")
BINARY  := ~/.local/bin/anito
BUILD   := go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY) ./cmd/anito/
UI_DIR  := internal/server/ui

PLIST := $(HOME)/Library/LaunchAgents/com.anito.daemon.plist

.PHONY: build install reload start stop release test coverage coverage-check ui-build ui-dev install-hooks

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

## coverage: run tests, print per-package table, append snapshot to .coverage/history.txt
coverage:
	@bash scripts/coverage

## coverage-check: same as coverage but fails if any package is below its floor in .coverage/floors.txt
coverage-check:
	@CHECK=1 bash scripts/coverage

## install-hooks: install git hooks from scripts/ into .git/hooks/
install-hooks:
	cp scripts/pre-commit .git/hooks/pre-commit
	chmod +x .git/hooks/pre-commit
	@echo "installed pre-commit hook"

## help: list targets
help:
	@grep -E '^## ' Makefile | sed 's/## //'
