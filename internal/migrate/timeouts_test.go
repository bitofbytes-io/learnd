package migrate

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"testing/fstest"
	"time"

	"github.com/drywaters/learnd/internal/testdb"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3/lock"
)

func testApply(ctx context.Context, pool *pgxpool.Pool, fsys fstest.MapFS, timeouts migrationTimeouts) error {
	return applyWithTimeouts(ctx, pool, fsys, discard, timeouts)
}

func holdMigrationLock(t *testing.T, pool *pgxpool.Pool) func() {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", lock.DefaultLockID); err != nil {
		conn.Release()
		t.Fatal(err)
	}
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, err := conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", lock.DefaultLockID)
		conn.Release()
		if err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(release)
	return release
}

func TestApplyLockWaitIsBounded(t *testing.T) {
	for _, mode := range []string{"lock deadline", "total deadline", "caller cancellation"} {
		t.Run(mode, func(t *testing.T) {
			pool := testdb.NewEmpty(t)
			release := holdMigrationLock(t, pool)
			timeouts := migrationTimeouts{total: 3 * time.Second, lock: 3 * time.Second, unlock: time.Second}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wantErr := context.DeadlineExceeded
			switch mode {
			case "lock deadline":
				timeouts.lock = 150 * time.Millisecond
			case "total deadline":
				timeouts.total = 150 * time.Millisecond
			case "caller cancellation":
				wantErr = context.Canceled
				timer := time.AfterFunc(150*time.Millisecond, cancel)
				defer timer.Stop()
			}
			fsys := fstest.MapFS{"001_probe.sql": {Data: []byte("-- +goose Up\nCREATE TABLE deadline_probe (id INT);\n")}}
			start := time.Now()
			err := testApply(ctx, pool, fsys, timeouts)
			if !errors.Is(err, wantErr) {
				t.Fatalf("apply error = %v, want %v", err, wantErr)
			}
			if elapsed := time.Since(start); elapsed > 2*time.Second {
				t.Fatalf("lock wait exceeded test budget: %v", elapsed)
			}
			var versionTable *string
			if err := pool.QueryRow(context.Background(), "SELECT to_regclass('goose_db_version')::text").Scan(&versionTable); err != nil {
				t.Fatal(err)
			}
			// Goose may initialize its version table while checking for pending
			// work, but it must not record or execute a migration without the lock.
			if versionTable != nil && len(appliedVersions(t, pool)) != 0 {
				t.Fatal("lock waiter recorded a migration before acquiring the lock")
			}
			var probeTable *string
			if err := pool.QueryRow(context.Background(), "SELECT to_regclass('deadline_probe')::text").Scan(&probeTable); err != nil {
				t.Fatal(err)
			}
			if probeTable != nil {
				t.Fatal("lock waiter executed migration DDL")
			}
			release()
			if err := testApply(context.Background(), pool, fsys, startupTimeouts); err != nil {
				t.Fatalf("retry after releasing lock: %v", err)
			}
			if got := appliedVersions(t, pool); len(got) != 1 || got[0] != 1 {
				t.Fatalf("versions = %v, want [1]", got)
			}
		})
	}
}

func TestApplyInterruptedSQLRollsBackAndReleasesLock(t *testing.T) {
	for _, mode := range []string{"total deadline", "caller cancellation"} {
		t.Run(mode, func(t *testing.T) {
			pool := testdb.NewEmpty(t)
			first := fstest.MapFS{"001_probe.sql": {Data: []byte("-- +goose Up\nCREATE TABLE deadline_probe (id INT);\n")}}
			if err := testApply(context.Background(), pool, first, startupTimeouts); err != nil {
				t.Fatal(err)
			}
			pending := fstest.MapFS{
				"001_probe.sql": first["001_probe.sql"],
				"002_slow.sql":  {Data: []byte("-- +goose Up\nALTER TABLE deadline_probe ADD COLUMN note TEXT;\nSELECT pg_sleep(10) /* migration_cancel_probe */;\n")},
			}
			timeouts := migrationTimeouts{total: 500 * time.Millisecond, lock: time.Second, unlock: time.Second}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wantErr := context.DeadlineExceeded
			if mode == "caller cancellation" {
				timeouts.total = 5 * time.Second
				wantErr = context.Canceled
			}
			done := make(chan error, 1)
			start := time.Now()
			go func() { done <- testApply(ctx, pool, pending, timeouts) }()
			// Observe the SQL in PostgreSQL so cancellation definitely interrupts the
			// migration itself, after its ALTER TABLE and lock acquisition.
			waitCtx, stopWait := context.WithTimeout(context.Background(), 3*time.Second)
			defer stopWait()
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				var active bool
				if err := pool.QueryRow(waitCtx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname = current_database() AND pid <> pg_backend_pid() AND state = 'active' AND query LIKE '%pg_sleep(10)%migration_cancel_probe%')`).Scan(&active); err != nil {
					t.Fatal(err)
				}
				if active {
					break
				}
				select {
				case err := <-done:
					t.Fatalf("migration ended before slow SQL was observed: %v", err)
				case <-waitCtx.Done():
					t.Fatal("slow SQL never became active")
				case <-ticker.C:
				}
			}
			if mode == "caller cancellation" {
				cancel()
			}
			select {
			case err := <-done:
				if !errors.Is(err, wantErr) {
					t.Fatalf("apply error = %v, want %v", err, wantErr)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("interrupted migration did not stop")
			}
			if elapsed := time.Since(start); elapsed > 3*time.Second {
				t.Fatalf("migration exceeded test budget: %v", elapsed)
			}
			if got := appliedVersions(t, pool); len(got) != 1 || got[0] != 1 {
				t.Fatalf("versions = %v, want [1]", got)
			}
			var columns int
			if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'deadline_probe' AND column_name = 'note'").Scan(&columns); err != nil {
				t.Fatal(err)
			}
			if columns != 0 {
				t.Fatal("canceled migration left partial DDL")
			}
			pending["002_slow.sql"].Data = []byte("-- +goose Up\nALTER TABLE deadline_probe ADD COLUMN note TEXT;\n")
			// A distinct pool proves the failed migration did not retain a session lock.
			retryPool, err := pgxpool.NewWithConfig(context.Background(), pool.Config())
			if err != nil {
				t.Fatal(err)
			}
			defer retryPool.Close()
			retryCtx, stopRetry := context.WithTimeout(context.Background(), 3*time.Second)
			defer stopRetry()
			if err := testApply(retryCtx, retryPool, pending, startupTimeouts); err != nil {
				t.Fatalf("retry after interruption: %v", err)
			}
			if got := appliedVersions(t, pool); len(got) != 2 || got[1] != 2 {
				t.Fatalf("retry versions = %v, want [1 2]", got)
			}
		})
	}
}

type waitingLocker struct{ unlockStarted chan bool }

func (l waitingLocker) SessionLock(ctx context.Context, _ *sql.Conn) error {
	<-ctx.Done()
	return ctx.Err()
}
func (l waitingLocker) SessionUnlock(ctx context.Context, _ *sql.Conn) error {
	l.unlockStarted <- ctx.Err() == nil
	<-ctx.Done()
	return ctx.Err()
}

func TestLockerBoundsDetachedCleanup(t *testing.T) {
	started := make(chan bool, 1)
	locker := deadlineLocker{locker: waitingLocker{unlockStarted: started}, timeouts: migrationTimeouts{lock: 20 * time.Millisecond, unlock: 20 * time.Millisecond}}
	if err := locker.SessionLock(context.Background(), nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := locker.SessionUnlock(ctx, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unlock: %v", err)
	}
	if !<-started {
		t.Fatal("cleanup inherited caller cancellation")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("cleanup exceeded budget: %v", elapsed)
	}
}

func TestApplyWaitsForAdvisoryLock(t *testing.T) {
	pool := testdb.NewEmpty(t)
	release := holdMigrationLock(t, pool)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Apply(ctx, pool, discard) }()
	select {
	case err := <-done:
		t.Fatalf("Apply finished while the lock was held: %v", err)
	case <-time.After(1500 * time.Millisecond):
	}
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("apply after unlock: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("Apply did not finish after lock release")
	}
	if got := appliedVersions(t, pool); len(got) != embeddedCount(t) {
		t.Fatalf("versions = %v, want all embedded versions", got)
	}
}
