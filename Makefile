GO      ?= go
BINDIR  ?= bin
PKG     := github.com/gremlyn-ai/gremlyn
CLIPKG  := $(PKG)/internal/cli

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X $(CLIPKG).Version=$(VERSION) \
	-X $(CLIPKG).Commit=$(COMMIT) \
	-X $(CLIPKG).BuildDate=$(DATE)

GOBUILD := CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)"

.PHONY: all build test test-verbose integration lint vet fmt \
        coverage check clean tidy vulncheck plugin-check

all: build

build:
	$(GOBUILD) -o $(BINDIR)/gremlyn ./cmd/gremlyn

test:
	$(GO) test ./... -race -coverprofile=coverage.out

test-verbose:
	$(GO) test ./... -race -v

integration:
	$(GO) test -tags=integration ./... -race

lint:
	golangci-lint run

vulncheck:
	@command -v govulncheck >/dev/null 2>&1 || { \
		echo "installing govulncheck..."; \
		$(GO) install golang.org/x/vuln/cmd/govulncheck@latest; \
	}
	$(shell $(GO) env GOPATH)/bin/govulncheck ./... || govulncheck ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w .
	@command -v goimports >/dev/null && goimports -w . || echo "goimports not installed, skipped"

coverage: test
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "coverage.html written"

check: vet lint test vulncheck
	@echo "All checks passed"

tidy:
	$(GO) mod tidy

plugin-check:
	claude plugin validate .
	sh -n scripts/gremlyn
	sh -n install.sh

clean:
	rm -rf $(BINDIR) dist coverage.out coverage.html
