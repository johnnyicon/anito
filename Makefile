VERSION ?= $(shell git describe --tags --exact-match 2>/dev/null || echo "dev")
BINARY  := ~/.local/bin/anito
BUILD   := go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY) ./cmd/anito/

.PHONY: build install reload release test

## build: compile the binary to ~/.local/bin/anito
build:
	$(BUILD)
	@echo "built anito $(VERSION) → $(BINARY)"

## install: alias for build
install: build

## reload: build + reload the running daemon (requires launchd agent installed)
reload: build
	anito reload

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
