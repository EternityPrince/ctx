APP_NAME := ctx
BUILD_DIR := $(CURDIR)/bin
GOCACHE_DIR := $(CURDIR)/.cache/go-build

.PHONY: build test run install uninstall

build:
	@mkdir -p "$(BUILD_DIR)" "$(GOCACHE_DIR)"
	GOCACHE="$(GOCACHE_DIR)" go build -o "$(BUILD_DIR)/$(APP_NAME)" ./cmd/ctx

test:
	@mkdir -p "$(GOCACHE_DIR)"
	GOCACHE="$(GOCACHE_DIR)" go test ./...

run:
	@mkdir -p "$(GOCACHE_DIR)"
	GOCACHE="$(GOCACHE_DIR)" go run ./cmd/ctx .

install:
	./scripts/install.sh

uninstall:
	./scripts/uninstall.sh
