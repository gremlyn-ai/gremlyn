# Gremlyn Core — Shared proxy engine & CLI
 
## Project overview
Gremlyn Core is the shared Go library that provides the MCP (Model Context Protocol) proxy engine used by both Gremlyn Shield (firewall) and Gremlyn Arena (chaos testing). It also contains the main `gremlyn` CLI binary.
 
This is a **library first** — Shield and Arena import `pkg/` packages. The CLI in `cmd/gremlyn/` orchestrates both services.
 
## Tech stack
- Go 1.22+
- JSON-RPC 2.0 over stdio and HTTP/SSE
- No web framework needed for the proxy (net/http + goroutines)
- cobra for CLI
- zerolog for structured logging
- YAML parsing: gopkg.in/yaml.v3
 
## Architecture
- `pkg/` contains all public packages (importable by shield/arena repos)
- `internal/` contains CLI-specific code only
- `cmd/gremlyn/` is the CLI entry point
 
## Code style — IMPORTANT
- All types MUST be strongly typed with Go structs. Never use map[string]interface{} for known data structures.
- Every exported function MUST have a godoc comment.
- Every struct field MUST have a json tag and a yaml tag where applicable.
- Error handling: always wrap errors with fmt.Errorf("context: %w", err). Never discard errors silently.
- Use custom error types for domain errors (e.g., ErrPolicyViolation, ErrProxyTimeout).
- Prefer interfaces for testability. Define interfaces in the consumer package, not the provider.
- No global state. Pass dependencies via constructor injection.
- Context: every function that does I/O MUST accept context.Context as first parameter.
- Naming: use Go conventions — camelCase for private, PascalCase for public, short receiver names (p for Proxy, c for Config).
 
## Project structure
```
cmd/gremlyn/main.go          → CLI entry (cobra root command)
pkg/proxy/proxy.go            → Core proxy interface + factory
pkg/proxy/wrap.go             → Stdio wrap mode (local MCP servers)
pkg/proxy/httpproxy.go        → HTTP/SSE reverse proxy mode
pkg/proxy/jsonrpc.go          → JSON-RPC 2.0 message parser
pkg/proxy/pipeline.go         → Analysis pipeline (hook system for shield/arena)
pkg/config/config.go          → gremlyn.yaml parser
pkg/config/mcpconfig.go       → MCP client config detection + rewrite
pkg/protocol/messages.go      → MCP message types (ToolCall, ToolResult, etc.)
pkg/protocol/transport.go     → Transport abstraction (stdio vs HTTP)
pkg/models/models.go          → Shared domain models
internal/cli/                 → CLI commands (init, status, doctor, version)
```
 
## Commands
```bash
go build -o gremlyn ./cmd/gremlyn      # Build CLI binary
go test ./...                           # Run all tests
go test ./pkg/proxy/ -v                 # Run proxy tests
go vet ./...                            # Static analysis
golangci-lint run                       # Linting
```
 
## Testing rules
- Every package MUST have a _test.go file.
- Use table-driven tests for all functions with multiple input scenarios.
- Use testify/assert for assertions, testify/require for fatal checks.
- Mock external dependencies with interfaces, not concrete types.
- Test file naming: foo_test.go next to foo.go.
- Integration tests in a separate _integration_test.go with build tag //go:build integration.
 
## Git workflow
- Branch naming: feat/xxx, fix/xxx, refactor/xxx
- Commit messages: conventional commits (feat:, fix:, refactor:, test:, docs:)
- Always run `go vet ./... && go test ./...` before committing.