-- Gremlyn Arena schema

CREATE TABLE arena_sessions (
    id              UUID PRIMARY KEY,
    server_id       VARCHAR(255) NOT NULL,
    status          VARCHAR(20)  NOT NULL CHECK (status IN ('running', 'completed', 'cancelled')),
    config          JSONB        NOT NULL DEFAULT '{}',
    results         JSONB,
    gremlins_sent   INTEGER      NOT NULL DEFAULT 0,
    gremlins_survived INTEGER    NOT NULL DEFAULT 0,
    gremlins_crashed  INTEGER    NOT NULL DEFAULT 0,
    started_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ
);

CREATE INDEX idx_sessions_status ON arena_sessions (status);
CREATE INDEX idx_sessions_server ON arena_sessions (server_id);
CREATE INDEX idx_sessions_started ON arena_sessions (started_at DESC);

CREATE TABLE arena_events (
    id              UUID PRIMARY KEY,
    session_id      UUID         NOT NULL REFERENCES arena_sessions(id) ON DELETE CASCADE,
    gremlin_type    VARCHAR(50)  NOT NULL,
    gremlin_config  JSONB        NOT NULL DEFAULT '{}',
    injected_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    agent_response  JSONB,
    outcome         VARCHAR(20)  NOT NULL CHECK (outcome IN ('survived', 'crashed', 'degraded')),
    score           INTEGER      NOT NULL DEFAULT 0 CHECK (score >= 0 AND score <= 100),
    details         JSONB
);

CREATE INDEX idx_events_session ON arena_events (session_id);
CREATE INDEX idx_events_gremlin ON arena_events (gremlin_type);
CREATE INDEX idx_events_outcome ON arena_events (outcome);
CREATE INDEX idx_events_injected ON arena_events (injected_at);
