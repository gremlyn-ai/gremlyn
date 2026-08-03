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

.PHONY: all build gremlyn shield arena test test-verbose integration lint vet fmt \
        coverage check clean run-shield run-arena dashboard-check tidy

all: build

## build — all three binaries into $(BINDIR)/
build: gremlyn shield arena

gremlyn:
	$(GOBUILD) -o $(BINDIR)/gremlyn ./cmd/gremlyn

shield:
	$(GOBUILD) -o $(BINDIR)/shield ./cmd/shield

arena:
	$(GOBUILD) -o $(BINDIR)/arena ./cmd/arena

## test — unit tests with the race detector (the gate)
test:
	$(GO) test ./... -race -coverprofile=coverage.out

test-verbose:
	$(GO) test ./... -race -v

## integration — needs docker compose up -d postgres redis
integration:
	$(GO) test -tags=integration ./... -race

lint:
	golangci-lint run

vet:
	$(GO) vet ./...

fmt:
	gofmt -w .
	@command -v goimports >/dev/null && goimports -w . || echo "goimports not installed, skipped"

coverage: test
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "coverage.html written"

## check — THE gate. Run before every commit.
check: vet lint test
	@echo "All checks passed"

tidy:
	$(GO) mod tidy

## dashboard-check — the frontend gate
dashboard-check:
	cd dashboard && npm run typecheck && npm run lint && npx vitest run && npm run build

run-shield: shield
	./$(BINDIR)/shield

run-arena: arena
	./$(BINDIR)/arena

clean:
	rm -rf $(BINDIR) dist coverage.out coverage.html
