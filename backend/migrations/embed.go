// Package migrations embeds the database schema, applied in filename order at
// startup by the PostgreSQL adapter.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS

// DownFS holds, for the migrations that have one, the script that undoes it
// (down/<version>.down.sql). It is never applied at startup: it exists so the
// tests prove each migration can be undone and applied again, and to roll back by hand.
//
//go:embed down/*.sql
var DownFS embed.FS
