// Package migrate applies the embedded SQL migration chain against the
// admin pool at server boot. There is no external migration binary in the
// distroless runtime image (see Dockerfile), so this replaces what a
// golang-migrate CLI step would otherwise do in a separate deploy stage.
package migrate

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/migrations"
)

// Run applies every *.up.sql file embedded in migrations.FS that is not yet
// recorded in schema_migrations, in filename order, each in its own
// transaction. Safe to call on every boot: already-applied files are
// skipped, so a fresh container and a warm restart both converge on the
// same schema.
func Run(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) error {
	if pool == nil {
		return fmt.Errorf("migrate: pool is nil")
	}

	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		filename   TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("migrate: ensure schema_migrations: %w", err)
	}

	names, err := fs.Glob(migrations.FS, "*.up.sql")
	if err != nil {
		return fmt.Errorf("migrate: glob embedded migrations: %w", err)
	}
	sort.Strings(names)

	rows, err := pool.Query(ctx, `SELECT filename FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("migrate: load applied: %w", err)
	}
	applied := make(map[string]bool, len(names))
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			rows.Close()
			return fmt.Errorf("migrate: scan applied: %w", err)
		}
		applied[f] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("migrate: iterate applied: %w", err)
	}

	appliedNow := 0
	for _, name := range names {
		if applied[name] {
			continue
		}
		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("migrate: read %s: %w", name, err)
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("migrate: begin tx for %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migrate: apply %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (filename) VALUES ($1)`, name); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migrate: record %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("migrate: commit %s: %w", name, err)
		}

		logger.Info("migration applied", "file", name)
		appliedNow++
	}

	if appliedNow == 0 {
		logger.Info("migrations up to date", "total", len(names))
	} else {
		logger.Info("migrations complete", "applied", appliedNow, "total", len(names))
	}
	return nil
}
