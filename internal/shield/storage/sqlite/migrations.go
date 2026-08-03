package sqlite

import _ "embed"

//go:embed migrations/001_init.up.sql
var migrationSQL string

//go:embed migrations/002_servers.up.sql
var migrationServersSQL string
