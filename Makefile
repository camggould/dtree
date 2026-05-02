.DEFAULT_GOAL := help

# Version metadata injected into the binary at build time.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X 'github.com/cgould/dtree/internal/cli.buildVersion=$(VERSION)' \
           -X 'github.com/cgould/dtree/internal/cli.buildCommit=$(COMMIT)' \
           -X 'github.com/cgould/dtree/internal/cli.buildDate=$(DATE)'

.PHONY: help
help:  ## Show this help.
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: setup
setup:  ## Install Go dependencies.
	go mod download

.PHONY: build
build:  ## Build the dtree binary.
	go build -ldflags="$(LDFLAGS)" -o dtree ./cmd/dtree

.PHONY: test
test:  ## Run all Go tests.
	go test ./...

.PHONY: lint
lint:  ## Run go vet.
	go vet ./...

.PHONY: clean
clean:  ## Remove build artifacts.
	rm -f dtree
