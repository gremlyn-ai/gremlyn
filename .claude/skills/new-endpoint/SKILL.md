---
name: new-endpoint
description: Create a new REST endpoint in Shield or Arena — chi handler, DTOs, service method, both repositories, tests, and the matching dashboard TypeScript type. Use when the user says "new endpoint", "add an API route", "expose X in the API", or runs /new-endpoint.
---

# New API Endpoint

Create a new chi endpoint following Gremlyn conventions. The work spans **five layers plus the dashboard type** — an endpoint that stops at the handler is not done.

## Arguments

- `service`: `shield` (:8081) or `arena` (:8082)
- `resource`: e.g. `servers`, `sessions`, `rules`
- `method`: GET / POST / PUT / DELETE

## Layers — all of them

```
internal/<product>/api/router.go        → register the route
internal/<product>/api/types.go         → request + response DTOs
internal/<product>/api/handlers.go      → thin handler: parse → service → respond
internal/<product>/service/<svc>.go     → the business logic + the store interface it needs
internal/<product>/storage/sqlite/      → repository implementation (DEFAULT store)
internal/<product>/storage/postgres/    → repository implementation (same interface)
migrations (both stores)      → only if the schema moved
─────────────────────────────────────────────────────────────────
dashboard/lib/api/types.ts    → mirror the Go DTO exactly
dashboard/lib/api/<svc>.ts    → the client method
```

## Checklist

### 1. DTOs — `internal/<product>/api/types.go`
- [ ] Request and response structs, **separate from the domain models**
- [ ] `json` tag on every field
- [ ] Godoc on every exported type
- [ ] No `map[string]interface{}` for a known shape
- [ ] Optional fields are pointers or `omitempty` — and you'll mirror that optionality in TypeScript

### 2. Service — `internal/<product>/service/<svc>.go`
- [ ] Method takes `ctx context.Context` **first**
- [ ] **Defines the store interface it needs, here in the consumer** — 1–3 methods
- [ ] All business logic lives here, not in the handler
- [ ] Errors wrapped: `fmt.Errorf("list servers: %w", err)`
- [ ] Domain errors as sentinels so the handler can map them to status codes

### 3. Repositories — **both stores**
- [ ] `internal/<product>/storage/sqlite/<resource>_repo.go` — `?` placeholders
- [ ] `internal/<product>/storage/postgres/<resource>_repo.go` — `$1` placeholders
- [ ] **Parameterized always.** Never `fmt.Sprintf` into SQL
- [ ] `defer rows.Close()` and **`rows.Err()` checked** after the loop
- [ ] Dialect differences (booleans, timestamps, JSON) normalized *inside* the repo — never leaked to the service
- [ ] For a list endpoint on a growing table: **keyset pagination, never `OFFSET`**, and always a bound

### 4. Handler — `internal/<product>/api/handlers.go`
- [ ] **Thin**: decode → validate → call service → encode
- [ ] Validate the request DTO before it reaches the service
- [ ] Error response shape: `{ "error": "message", "code": "ERROR_CODE" }`
- [ ] Map domain errors to status codes (`errors.Is`)
- [ ] Never log a payload — log identifiers

### 5. Route — `internal/<product>/api/router.go`
- [ ] RESTful path under `/api/v1/`
- [ ] **API key auth middleware applied** — no exceptions without a written reason
- [ ] Registered inside the existing route group so middleware actually wraps it

### 6. Tests — `internal/<product>/api/handlers_test.go`
Through the **real chi router** with `httptest`, not the handler in isolation:
- [ ] Success: status + response shape
- [ ] Validation error: shape and code
- [ ] **Auth: valid key → 200, invalid key → 401, missing key → 401**
- [ ] Table-driven, `testify/require` + `assert`
- [ ] Repository test run against **both** SQLite and PostgreSQL via the same interface test
- [ ] `go test -race ./...` clean

### 7. Dashboard contract — don't skip this
- [ ] `lib/api/types.ts` — mirror the Go DTO **exactly**: field names from the `json` tags, matching optionality (`*string`/`omitempty` → `field?: string`)
- [ ] `lib/api/<svc>.ts` — the client method, going through `lib/api/client.ts`
- [ ] `npm run typecheck` clean

Skipping this leaves the two repos disagreeing about the API, and TypeScript won't catch it until someone tries to use the field.

## Templates

### Handler
```go
// ListServers returns the MCP servers registered behind the proxy.
//
// GET /api/v1/servers
func (h *Handler) ListServers(w http.ResponseWriter, r *http.Request) {
	servers, err := h.svc.ListServers(r.Context())
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to list servers")
		h.log.Error().Err(err).Msg("list servers")
		return
	}
	h.writeJSON(w, http.StatusOK, toServerDTOs(servers))
}
```

### Service + consumer-owned interface
```go
// ServerStore reads registered MCP servers.
type ServerStore interface {
	List(ctx context.Context) ([]models.Server, error)
}

// ListServers returns every registered MCP server.
func (s *ShieldService) ListServers(ctx context.Context) ([]models.Server, error) {
	servers, err := s.servers.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list servers: %w", err)
	}
	return servers, nil
}
```

### SQLite repository
```go
// List returns every registered server, newest first.
func (r *ServersRepo) List(ctx context.Context) ([]models.Server, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, transport, created_at FROM servers ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query servers: %w", err)
	}
	defer rows.Close()

	var out []models.Server
	for rows.Next() {
		var s models.Server
		if err := rows.Scan(&s.ID, &s.Name, &s.Transport, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan server: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
```

### Handler test
```go
func TestListServers(t *testing.T) {
	tests := []struct {
		name     string
		apiKey   string
		wantCode int
	}{
		{"ok", validKey, http.StatusOK},
		{"bad key", "nope", http.StatusUnauthorized},
		{"no key", "", http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/servers", nil)
			if tt.apiKey != "" {
				req.Header.Set("X-API-Key", tt.apiKey)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			require.Equal(t, tt.wantCode, rec.Code)
		})
	}
}
```

### TypeScript mirror
```ts
// lib/api/types.ts — mirrors models.Server / ServerDTO
export interface Server {
  id: string;
  name: string;
  transport: "stdio" | "http";
  created_at: string;
  last_seen_at?: string;   // Go *time.Time / omitempty
}
```

## Done means

```bash
# in the service repo
make check                                    # vet + lint + go test -race
# in the dashboard
npm run typecheck && npm run lint
```

Plus: both repositories implemented, auth tested, no `OFFSET` on a growing table, and the TypeScript type matching the Go DTO field for field.
