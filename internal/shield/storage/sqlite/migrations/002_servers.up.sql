-- Servers table: registered MCP servers managed via UI or API.

CREATE TABLE IF NOT EXISTS servers (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    mode        TEXT NOT NULL DEFAULT 'proxy' CHECK (mode IN ('proxy', 'wrap', 'cloud')),
    upstream_url TEXT,
    command     TEXT,
    args        TEXT DEFAULT '[]',
    env         TEXT DEFAULT '{}',
    auth_header TEXT DEFAULT '',
    headers     TEXT DEFAULT '{}',
    status      TEXT NOT NULL DEFAULT 'inactive' CHECK (status IN ('active', 'inactive', 'error')),
    last_seen   TEXT,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_servers_name ON servers (name);
CREATE INDEX IF NOT EXISTS idx_servers_status ON servers (status);
