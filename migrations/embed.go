package migrations

import "embed"

// Files contains Pi-Ramid's forward-only database migrations.
//
//go:embed *.sql
var Files embed.FS
