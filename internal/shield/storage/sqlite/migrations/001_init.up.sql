-- Gremlyn Shield schema (SQLite)

CREATE TABLE IF NOT EXISTS events (
    id              TEXT PRIMARY KEY,
    server_id       TEXT NOT NULL,
    session_id      TEXT,
    direction       TEXT NOT NULL CHECK (direction IN ('outgoing', 'incoming')),
    message_type    TEXT NOT NULL,
    tool_name       TEXT,
    tool_args       TEXT,
    response_payload TEXT,
    payload_ref     TEXT,
    action_taken    TEXT NOT NULL CHECK (action_taken IN ('allowed', 'blocked', 'redacted', 'alerted')),
    rules_triggered TEXT DEFAULT '[]',
    detection_results TEXT DEFAULT '{}',
    latency_ms      INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_server_id ON events (server_id);
CREATE INDEX IF NOT EXISTS idx_events_action_taken ON events (action_taken);
CREATE INDEX IF NOT EXISTS idx_events_created_at ON events (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_events_tool_name ON events (tool_name) WHERE tool_name IS NOT NULL;

CREATE TABLE IF NOT EXISTS rules (
    id              TEXT PRIMARY KEY,
    server_id       TEXT,
    name            TEXT NOT NULL,
    match_config    TEXT,
    scan_responses  INTEGER NOT NULL DEFAULT 0,
    scan_outgoing   INTEGER NOT NULL DEFAULT 0,
    detect          TEXT,
    entity_field    TEXT,
    action          TEXT NOT NULL CHECK (action IN ('allow', 'block', 'block_and_alert', 'redact', 'redact_and_alert', 'throttle', 'pause_and_request_approval', 'log_only')),
    enabled         INTEGER NOT NULL DEFAULT 1,
    source          TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'template', 'natural_language')),
    original_text   TEXT,
    config          TEXT,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_rules_server_id ON rules (server_id);
CREATE INDEX IF NOT EXISTS idx_rules_enabled ON rules (enabled) WHERE enabled = 1;

CREATE TABLE IF NOT EXISTS alerts (
    id              TEXT PRIMARY KEY,
    event_id        TEXT REFERENCES events(id),
    severity        TEXT NOT NULL CHECK (severity IN ('critical', 'high', 'medium', 'low')),
    type            TEXT NOT NULL,
    message         TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'new' CHECK (status IN ('new', 'acknowledged', 'resolved', 'false_positive')),
    notified_channels TEXT DEFAULT '[]',
    created_at      TEXT NOT NULL,
    resolved_at     TEXT
);

CREATE INDEX IF NOT EXISTS idx_alerts_severity ON alerts (severity);
CREATE INDEX IF NOT EXISTS idx_alerts_status ON alerts (status);
CREATE INDEX IF NOT EXISTS idx_alerts_created_at ON alerts (created_at DESC);
