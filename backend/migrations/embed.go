// Package migrations carries the SQL schema migrations as an embedded
// filesystem, so that cmd/migrate is a single static binary with no
// dependency on the source tree at run time.
//
// Migrations are forward-only (07_deployment_view.md §7.4). No file in this
// directory declares a `-- +goose Down` section, which means `goose down`
// fails rather than half-reversing a schema. To reset a development database,
// drop it and migrate again.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
