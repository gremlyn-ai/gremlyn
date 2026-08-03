# Gremlyn Shield — MCP Firewall microservice
 
## Project overview
Gremlyn Shield is the firewall/defense microservice. It imports gremlyn-core/pkg/ as a Go module dependency. It intercepts MCP traffic via the core proxy engine and applies security policies, prompt injection detection, PII redaction, and behavioral monitoring.
 
Runs as a standalone HTTP server exposing a REST API on port 8081.
 
## Tech stack
- Go 1.22+
- Imports: github.com/gremlyn-ai/gremlyn-core/pkg/...
- HTTP API: chi router (lightweight, idiomatic Go)
- Database: SQLite by default (~/.gremlyn/shield.db via modernc.org/sqlite, pure Go), PostgreSQL 16 optional via DATABASE_URL env var (pgx/v5)
- Migrations: embedded SQL auto-applied on startup (SQLite), golang-migrate/migrate (PostgreSQL)
- Cache/rate-limiting: Redis via go-redis/v9
- ML detection sidecar: Python FastAPI (separate Docker container)
- Structured logging: zerolog
- Config: gremlyn-shield.yaml (viper)
 
## Architecture
This is a microservice, NOT a library. It has its own:
- HTTP API server (REST endpoints for dashboard consumption)
- Database migrations and storage layer
- Detection pipeline (regex → ML → LLM-as-judge)
- Policy engine
- Alert system (Slack, email, webhook)
 
The ML detection model runs as a Python sidecar called via HTTP.
 
## Code style — IMPORTANT
- Same Go conventions as gremlyn-core. All types strongly typed with structs.
- Database layer: use raw SQL. NO ORM. Queries in dedicated repository files.
- Every SQL query MUST use parameterized queries ($1/$2 for pgx, ? for SQLite). Never string concatenation.
- Repository pattern: storage/postgres/ and storage/sqlite/ each contain one file per domain (events_repo.go, rules_repo.go, alerts_repo.go). Both implement the same service.EventStore/RuleStore/AlertStore interfaces.
- Service layer: internal/service/ contains business logic. Services accept repository interfaces, not concrete types.
- HTTP handlers: thin handlers in internal/api/. Handler calls service, service calls repo. No business logic in handlers.
- Request/response types: define in internal/api/types.go. Separate from domain models.
- Error responses: always JSON format { "error": "message", "code": "ERROR_CODE" }.
 
## Project structure
```
cmd/shield/main.go                      → Service entry point
internal/service/shield.go              → Main service orchestration
internal/api/router.go                  → Chi router setup
internal/api/handlers/events.go         → Event endpoints
internal/api/handlers/rules.go          → Rule CRUD endpoints
internal/api/handlers/alerts.go         → Alert endpoints
internal/api/handlers/shield.go         → Start/stop/status
internal/api/middleware/auth.go         → API key auth middleware
internal/api/types.go                   → Request/response DTOs
internal/policy/engine.go               → Policy engine
internal/policy/matcher.go              → Rule matching
internal/policy/actions.go              → Block/redact/alert/throttle
internal/policy/ratelimit.go            → Rate limiting
internal/detection/regex.go             → Layer 1 detection
internal/detection/classifier.go        → Layer 2 ML client
internal/detection/llmjudge.go          → Layer 3 LLM-as-judge client
internal/detection/structural.go        → Layer 4 schema analysis
internal/detection/pii.go               → PII detection
internal/behavioral/profiler.go         → Agent behavioral profiling
internal/behavioral/anomaly.go          → Anomaly detection
internal/behavioral/rugpull.go          → Tool definition change detection
internal/alert/slack.go                 → Slack alerting
internal/alert/email.go                 → Email alerting
internal/alert/webhook.go               → Generic webhook
internal/storage/postgres/events.go     → Events repository (PostgreSQL)
internal/storage/postgres/rules.go      → Rules repository (PostgreSQL)
internal/storage/postgres/alerts.go     → Alerts repository (PostgreSQL)
internal/storage/sqlite/db.go           → SQLite DB factory + auto-migration
internal/storage/sqlite/events_repo.go  → Events repository (SQLite)
internal/storage/sqlite/rules_repo.go   → Rules repository (SQLite)
internal/storage/sqlite/alerts_repo.go  → Alerts repository (SQLite)
internal/storage/s3.go                  → S3 payload storage
migrations/                             → SQL migration files
detection-models/                       → Python ML sidecar (FastAPI)
```
 
## Commands
```bash
go build -o shield ./cmd/shield               # Build
go test ./...                                   # All tests
go test ./internal/policy/ -v                   # Policy tests
docker compose up -d postgres redis             # Start deps (only for PostgreSQL mode)
migrate -path migrations -database "postgres://..." up  # Run PostgreSQL migrations (SQLite auto-migrates)
docker compose up -d ml-sidecar                 # Start ML model server
```
 
## Testing rules
- Same as gremlyn-core conventions.
- Policy engine tests: test every action type (block, redact, alert, allow).
- Detection tests: include known prompt injection samples from public datasets.
- Database tests: use a real test PostgreSQL (docker compose up postgres-test).
- API tests: use httptest.NewRecorder for handler tests.
 
## Git workflow
- Same as gremlyn-core.