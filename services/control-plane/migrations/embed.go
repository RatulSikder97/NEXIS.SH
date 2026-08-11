// Package migrations embeds the forward SQL migration chain so it ships
// inside the compiled server binary (the distroless runtime image has no
// filesystem access to the source tree). internal/platform/migrate applies
// these at boot, in filename order, tracking progress in schema_migrations.
package migrations

import "embed"

//go:embed *.up.sql
var FS embed.FS
