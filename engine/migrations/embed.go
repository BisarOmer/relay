// Package migrations embeds the versioned SQLite schema so packaged builds do
// not depend on a migrations directory being shipped next to the binary.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
