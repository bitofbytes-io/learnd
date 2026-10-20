// Package migrate applies the embedded Goose migrations at startup.
package migrate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/drywaters/learnd/migrations"
)

// Apply runs any pending SQL migrations bundled with the binary. It uses the
// same goose_db_version table as the goose CLI (`make migrate`), so versions
// already applied by hand are never re-run.
func Apply(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) error {
	return apply(ctx, pool, migrations.Files, logger)
}

func apply(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, logger *slog.Logger) error {
	// Replicas can start together. A Postgres session advisory lock makes one
	// apply pending migrations while the others wait, then find nothing to do.
	// Waiters poll every second for up to five minutes.
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockTimeout(1, 300))
	if err != nil {
		return fmt.Errorf("migrate: create session locker: %w", err)
	}

	db := stdlib.OpenDBFromPool(pool)
	defer func() { _ = db.Close() }()

	provider, err := goose.NewProvider(goose.DialectPostgres, db, fsys,
		goose.WithSessionLocker(locker),
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		return fmt.Errorf("migrate: create goose provider: %w", err)
	}

	results, err := provider.Up(ctx)
	var partial *goose.PartialError
	if errors.As(err, &partial) {
		results = partial.Applied
	}
	for _, result := range results {
		logger.Info("migration applied",
			"source", path.Base(result.Source.Path),
			"duration", result.Duration)
	}
	if partial != nil && partial.Failed != nil {
		return fmt.Errorf("migrate: apply %s: %w", path.Base(partial.Failed.Source.Path), err)
	}
	if err != nil {
		return fmt.Errorf("migrate: goose up: %w", err)
	}

	version, err := provider.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("migrate: read goose version: %w", err)
	}
	if len(results) == 0 {
		logger.Info("database migrations up to date", "version", version)
	} else {
		logger.Info("database migrations applied", "count", len(results), "version", version)
	}
	return nil
}
