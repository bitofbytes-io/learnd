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
	return applyWithTimeouts(ctx, pool, fsys, logger, startupTimeouts)
}

// applyWithTimeouts shares one startup deadline across all Goose work while
// allowing regression tests to exercise expiry with short, isolated budgets.
func applyWithTimeouts(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, logger *slog.Logger, timeouts migrationTimeouts) (retErr error) {
	// One deadline includes connection acquisition, lock waiting, SQL, and the
	// final version check. Cleanup has a separate, bounded allowance.
	ctx, cancel := context.WithTimeout(ctx, timeouts.total)
	defer cancel()
	defer func() {
		if err := ctx.Err(); err != nil {
			// Do not log the SQL or connection details from a canceled operation.
			retErr = fmt.Errorf("migrate: startup interrupted: %w", err)
		}
	}()
	locker, err := lock.NewPostgresSessionLocker(
		lock.WithLockTimeout(1, retryCount(timeouts.lock)),
		lock.WithUnlockTimeout(1, retryCount(timeouts.unlock)),
	)
	if err != nil {
		return fmt.Errorf("migrate: create session locker: %w", err)
	}
	boundedLocker := deadlineLocker{locker: locker, timeouts: timeouts}

	db := stdlib.OpenDBFromPool(pool)
	defer func() { _ = db.Close() }()

	provider, err := goose.NewProvider(goose.DialectPostgres, db, fsys,
		goose.WithSessionLocker(boundedLocker),
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
