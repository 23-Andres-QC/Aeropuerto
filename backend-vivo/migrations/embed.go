// Package migrations embeds the schema of the live-camera database.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS

// DownFS holds the script that undoes each migration that has one (down/<version>.down.sql). It is never applied at
// startup: it exists to roll back by hand and so the tests can prove a migration can be undone.
//
//go:embed down/*.sql
var DownFS embed.FS
