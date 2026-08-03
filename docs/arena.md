# Gremlyn Arena — Chaos engineering microservice
 
## Project overview
Gremlyn Arena is the chaos testing microservice. It imports gremlyn-core/pkg/ as a Go module dependency. It injects controlled failures ("gremlins") into MCP agent pipelines and measures resilience.
 
Runs as a standalone HTTP server exposing a REST API on port 8082. Also serves WebSocket connections for real-time session streaming to the dashboard.
 
## Tech stack
- Go 1.22+
- Imports: github.com/gremlyn-ai/gremlyn-core/pkg/...
- HTTP API: chi router
- WebSocket: gorilla/websocket (for live session streaming to dashboard)
- Database: SQLite by default (~/.gremlyn/arena.db via modernc.org/sqlite, pure Go), PostgreSQL 16 optional via DATABASE_URL env var (pgx/v5)
- Migrations: embedded SQL auto-applied on startup (SQLite), golang-migrate/migrate (PostgreSQL)
- Structured logging: zerolog
- Config: gremlyn-arena.yaml (viper)
 
## Architecture
Each chaos test is a "session". A session:
1. Creates a proxy instance from gremlyn-core in "chaos mode"
2. Registers gremlins (failure injectors) into the proxy pipeline
3. Sends test prompts to the agent via the proxy
4. Gremlins intercept and alter messages at configured probability
5. Records every event (injected gremlin + agent response)
6. Scores resilience per dimension
7. Streams events via WebSocket to the dashboard in real-time
 
Every gremlin implements the Gremlin interface.
 
## Code style — IMPORTANT
- Same Go conventions as gremlyn-core. Strongly typed everything.
- Gremlin interface: every gremlin MUST implement the Gremlin interface defined in internal/gremlins/gremlin.go.
- Gremlin interface: Inject(ctx, message) → (modified_message, injected bool, error).
- Sessions are state machines: Created → Running → Completed | Cancelled.
- WebSocket: use JSON messages with a "type" field for event routing.
- Scoring: pure functions, no side effects. scoring/scorer.go takes []ArenaEvent and returns ResilienceReport.
 
## Project structure
```
cmd/arena/main.go                       → Service entry point
internal/service/arena.go               → Main service orchestration
internal/api/router.go                  → Chi router + WebSocket upgrade
internal/api/handlers/sessions.go       → Session CRUD + start/stop
internal/api/handlers/gremlins.go       → List available gremlins
internal/api/handlers/ws.go             → WebSocket handler for live streaming
internal/api/types.go                   → Request/response DTOs
internal/session/manager.go             → Session lifecycle management
internal/session/runner.go              → Execute gremlins against agent
internal/session/recorder.go            → Record events during session
internal/gremlins/gremlin.go            → Gremlin interface definition
internal/gremlins/registry.go           → Gremlin registry
internal/gremlins/hallucination.go      → HallucinationGremlin
internal/gremlins/latency.go            → LatencyGremlin
internal/gremlins/corruption.go         → CorruptionGremlin
internal/gremlins/loop.go               → LoopGremlin
internal/gremlins/injection.go          → InjectionGremlin
internal/gremlins/identity.go           → IdentityGremlin
internal/gremlins/overflow.go           → OverflowGremlin
internal/gremlins/timeout.go            → TimeoutGremlin
internal/scoring/scorer.go              → Resilience score calculator
internal/scoring/dimensions.go          → Score dimensions enum + weights
internal/scoring/report.go              → Report generation (JSON, PDF)
internal/storage/postgres/db.go          → PostgreSQL DB factory
internal/storage/postgres/sessions.go   → Sessions repository (PostgreSQL)
internal/storage/postgres/events.go     → Arena events repository (PostgreSQL)
internal/storage/sqlite/db.go           → SQLite DB factory + auto-migration
internal/storage/sqlite/sessions_repo.go → Sessions repository (SQLite)
internal/storage/sqlite/events_repo.go  → Events repository (SQLite)
internal/storage/s3.go                  → Session recordings
migrations/
ci/github-action/                       → GitHub Actions integration
```
 
## Commands
```bash
go build -o arena ./cmd/arena                  # Build
go test ./...                                   # All tests
go test ./internal/gremlins/ -v                 # Gremlin tests
go test ./internal/scoring/ -v                  # Scoring tests
docker compose up -d postgres                   # Start deps (only for PostgreSQL mode, SQLite is zero-config)
```
 
## Testing rules
- Every gremlin MUST have comprehensive tests with table-driven test cases.
- Scoring tests: test edge cases (all survived, all crashed, empty session, single gremlin).
- Session tests: test state machine transitions (Created → Running → Completed, Created → Cancelled).
- WebSocket tests: use gorilla/websocket test helpers.
 
## Git workflow
- Same as gremlyn-core.