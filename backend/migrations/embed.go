// Package migrations embeds the database schema, applied in filename order at
// startup by the PostgreSQL adapter.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
