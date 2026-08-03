package sqlite

import _ "embed"

//go:embed migrations/001_init.up.sql
var migrationSQL string
