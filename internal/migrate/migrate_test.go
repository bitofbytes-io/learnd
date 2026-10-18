package migrate

import (
	"context"
	"io"
	"io/fs"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/drywaters/learnd/internal/testdb"
	"github.com/drywaters/learnd/migrations"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

// migrationFS copies the embedded migrations, keeping files accepted by keep,
// and adds extra files.
func migrationFS(t *testing.T, keep func(name string) bool, extra map[string]string) fstest.MapFS {
	t.Helper()
	names, err := fs.Glob(migrations.Files, "*.sql")
	if err != nil || len(names) == 0 {
		t.Fatalf("embedded migrations: %v", err)
	}
	fsys := fstest.MapFS{}
	for _, name := range names {
		if keep != nil && !keep(name) {
			continue
		}
		data, err := fs.ReadFile(migrations.Files, name)
		if err != nil {
			t.Fatal(err)
		}
		fsys[name] = &fstest.MapFile{Data: data}
	}
	for name, body := range extra {
		fsys[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return fsys
}

func appliedVersions(t *testing.T, pool *pgxpool.Pool) []int64 {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT version_id FROM goose_db_version WHERE version_id > 0 AND is_applied ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var versions []int64
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return versions
}

func embeddedCount(t *testing.T) int {
	t.Helper()
	names, err := fs.Glob(migrations.Files, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	return len(names)
}

func TestApplyFreshDatabaseThenNoop(t *testing.T) {
	pool := testdb.NewEmpty(t)
	ctx := context.Background()

	if err := Apply(ctx, pool, discard); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	want := embeddedCount(t)
	if got := appliedVersions(t, pool); len(got) != want {
		t.Fatalf("applied versions = %v, want %d", got, want)
	}
	if _, err := pool.Exec(ctx, `SELECT id, tag, search FROM entries LIMIT 1`); err != nil {
		t.Fatalf("entries schema: %v", err)
	}

	if err := Apply(ctx, pool, discard); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if got := appliedVersions(t, pool); len(got) != want {
		t.Fatalf("second apply recorded versions again: %v", got)
	}
}

func TestApplyOnlyRunsPendingMigrations(t *testing.T) {
	pool := testdb.NewEmpty(t)
	ctx := context.Background()

	// Simulate a database migrated by the goose CLI before the latest file.
	names, err := fs.Glob(migrations.Files, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	latest := names[len(names)-1]
	old := migrationFS(t, func(name string) bool { return name != latest }, nil)
	if err := apply(ctx, pool, old, discard); err != nil {
		t.Fatalf("apply all but %s: %v", latest, err)
	}
	if got := appliedVersions(t, pool); len(got) != len(names)-1 {
		t.Fatalf("applied versions = %v, want %d", got, len(names)-1)
	}

	// Re-running earlier files would fail (for example CREATE TABLE entries).
	if err := Apply(ctx, pool, discard); err != nil {
		t.Fatalf("apply latest: %v", err)
	}
	if got := appliedVersions(t, pool); len(got) != len(names) {
		t.Fatalf("applied versions = %v, want %d", got, len(names))
	}
}

func TestApplyNewMigrationAndToleratesNewerDatabase(t *testing.T) {
	pool := testdb.NewEmpty(t)
	ctx := context.Background()

	next := migrationFS(t, nil, map[string]string{
		"999_noop.sql": "-- +goose Up\nSELECT 1;\n\n-- +goose Down\nSELECT 1;\n",
	})
	if err := apply(ctx, pool, next, discard); err != nil {
		t.Fatalf("apply with extra migration: %v", err)
	}
	got := appliedVersions(t, pool)
	if got[len(got)-1] != 999 {
		t.Fatalf("applied versions = %v, want 999 last", got)
	}

	// Rolling back to an image without 999 must still start.
	if err := Apply(ctx, pool, discard); err != nil {
		t.Fatalf("apply older file set: %v", err)
	}
}

func TestApplyFailsOnBrokenMigration(t *testing.T) {
	pool := testdb.NewEmpty(t)
	ctx := context.Background()

	broken := migrationFS(t, nil, map[string]string{
		"999_broken.sql": "-- +goose Up\nSELECT * FROM table_that_does_not_exist;\n",
	})
	err := apply(ctx, pool, broken, discard)
	if err == nil || !strings.Contains(err.Error(), "999_broken.sql") {
		t.Fatalf("apply error = %v, want failure naming 999_broken.sql", err)
	}
	got := appliedVersions(t, pool)
	if len(got) != embeddedCount(t) || got[len(got)-1] == 999 {
		t.Fatalf("applied versions = %v, want embedded set without 999", got)
	}
}

func TestApplyConcurrentStartups(t *testing.T) {
	pool := testdb.NewEmpty(t)
	ctx := context.Background()

	const replicas = 3
	errs := make([]error, replicas)
	var wg sync.WaitGroup
	for i := range replicas {
		replica, err := pgxpool.NewWithConfig(ctx, pool.Config())
		if err != nil {
			t.Fatal(err)
		}
		defer replica.Close()
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = Apply(ctx, replica, discard)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("replica %d: %v", i, err)
		}
	}
	if got := appliedVersions(t, pool); len(got) != embeddedCount(t) {
		t.Fatalf("applied versions = %v, want each embedded migration once", got)
	}
}
