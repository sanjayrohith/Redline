// Package migrations embeds the gateway's forward-only SQL migration files.
package migrations

import "embed"

// FS holds every "NNN_name.sql" migration file, loadable via db.LoadMigrations.
//
//go:embed *.sql
var FS embed.FS
