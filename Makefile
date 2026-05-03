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

# sqlite_fts5 enables FTS5 full-text search in the bundled SQLite amalgamation.
# It also requires libm for the BM25 ranking function (-lm is handled by
# the go-sqlite3 sqlite_fts5 build option).
GOTAGS ?= sqlite_fts5

.PHONY: build
build:  ## Build the dtree binary.
	go build -tags "$(GOTAGS)" -ldflags="$(LDFLAGS)" -o dtree ./cmd/dtree

.PHONY: test
test:  ## Run all Go tests.
	go test -tags "$(GOTAGS)" ./...

.PHONY: lint
lint:  ## Run go vet.
	go vet -tags "$(GOTAGS)" ./...

.PHONY: coverage
coverage:  ## Run tests with coverage and print per-package summary.
	go test -tags "$(GOTAGS)" -coverprofile=coverage.out ./...
	@echo ""
	@go tool cover -func=coverage.out | tail -1
	@echo "HTML report: go tool cover -html=coverage.out"

.PHONY: clean
clean:  ## Remove build artifacts.
	rm -f dtree coverage.out
