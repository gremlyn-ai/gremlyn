-- Gremlyn Shield schema — initial migration

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE events (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    server_id       TEXT NOT NULL,
    session_id      TEXT,
    direction       TEXT NOT NULL CHECK (direction IN ('outgoing', 'incoming')),
    message_type    TEXT NOT NULL,
    tool_name       TEXT,
    tool_args       JSONB,
    response_payload JSONB,
    payload_ref     TEXT,
    action_taken    TEXT NOT NULL CHECK (action_taken IN ('allowed', 'blocked', 'redacted', 'alerted')),
    rules_triggered JSONB DEFAULT '[]',
    detection_results JSONB DEFAULT '{}',
    latency_ms      INT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_events_server_id ON events (server_id);
CREATE INDEX idx_events_action_taken ON events (action_taken);
CREATE INDEX idx_events_created_at ON events (created_at DESC);
CREATE INDEX idx_events_tool_name ON events (tool_name) WHERE tool_name IS NOT NULL;

CREATE TABLE rules (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    server_id       TEXT,
    name            TEXT NOT NULL,
    match_config    JSONB,
    scan_responses  BOOLEAN NOT NULL DEFAULT FALSE,
    scan_outgoing   BOOLEAN NOT NULL DEFAULT FALSE,
    detect          JSONB,
    entity_field    TEXT,
    action          TEXT NOT NULL CHECK (action IN ('allow', 'block', 'block_and_alert', 'redact', 'redact_and_alert', 'throttle', 'pause_and_request_approval', 'log_only')),
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    source          TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'template', 'natural_language')),
    original_text   TEXT,
    config          JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_rules_server_id ON rules (server_id);
CREATE INDEX idx_rules_enabled ON rules (enabled) WHERE enabled = TRUE;

CREATE TABLE alerts (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    event_id        UUID REFERENCES events(id),
    severity        TEXT NOT NULL CHECK (severity IN ('critical', 'high', 'medium', 'low')),
    type            TEXT NOT NULL,
    message         TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'new' CHECK (status IN ('new', 'acknowledged', 'resolved', 'false_positive')),
    notified_channels TEXT[] DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at     TIMESTAMPTZ
);

CREATE INDEX idx_alerts_severity ON alerts (severity);
CREATE INDEX idx_alerts_status ON alerts (status);
CREATE INDEX idx_alerts_created_at ON alerts (created_at DESC);
