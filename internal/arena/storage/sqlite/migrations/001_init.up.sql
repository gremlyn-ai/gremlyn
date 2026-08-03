-- Gremlyn Arena schema (SQLite)

CREATE TABLE IF NOT EXISTS arena_sessions (
    id              TEXT PRIMARY KEY,
    server_id       TEXT NOT NULL,
    status          TEXT NOT NULL CHECK (status IN ('running', 'completed', 'cancelled')),
    config          TEXT NOT NULL DEFAULT '{}',
    results         TEXT,
    gremlins_sent   INTEGER NOT NULL DEFAULT 0,
    gremlins_survived INTEGER NOT NULL DEFAULT 0,
    gremlins_crashed  INTEGER NOT NULL DEFAULT 0,
    started_at      TEXT NOT NULL,
    completed_at    TEXT
);

CREATE INDEX IF NOT EXISTS idx_sessions_status ON arena_sessions (status);
CREATE INDEX IF NOT EXISTS idx_sessions_server ON arena_sessions (server_id);
CREATE INDEX IF NOT EXISTS idx_sessions_started ON arena_sessions (started_at DESC);

CREATE TABLE IF NOT EXISTS arena_events (
    id              TEXT PRIMARY KEY,
    session_id      TEXT NOT NULL REFERENCES arena_sessions(id) ON DELETE CASCADE,
    gremlin_type    TEXT NOT NULL,
    gremlin_config  TEXT NOT NULL DEFAULT '{}',
    injected_at     TEXT NOT NULL,
    agent_response  TEXT,
    outcome         TEXT NOT NULL CHECK (outcome IN ('survived', 'crashed', 'degraded')),
    score           INTEGER NOT NULL DEFAULT 0 CHECK (score >= 0 AND score <= 100),
    details         TEXT
);

CREATE INDEX IF NOT EXISTS idx_events_session ON arena_events (session_id);
CREATE INDEX IF NOT EXISTS idx_events_gremlin ON arena_events (gremlin_type);
CREATE INDEX IF NOT EXISTS idx_events_outcome ON arena_events (outcome);
CREATE INDEX IF NOT EXISTS idx_events_injected ON arena_events (injected_at);
