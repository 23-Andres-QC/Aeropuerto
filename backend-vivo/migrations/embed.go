// Package migrations embeds the schema of the live-camera database.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
