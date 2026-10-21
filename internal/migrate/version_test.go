package migrate

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/drywaters/learnd/internal/testdb"
)

// migrationLogWriter lets the test interpose after Goose releases its lock,
// before Apply reads the final version for its success log.
type migrationLogWriter func([]byte) (int, error)

func (write migrationLogWriter) Write(p []byte) (int, error) { return write(p) }

func TestApplyDoesNotReacquireLockForVersionLog(t *testing.T) {
	pool := testdb.NewEmpty(t)
	var release func()
	logger := slog.New(slog.NewTextHandler(migrationLogWriter(func(p []byte) (int, error) {
		if release == nil && strings.Contains(string(p), "migration applied") {
			// Simulate another replica acquiring the lock after our migration ends.
			// If the version read reacquires it, Apply will exhaust the short budget.
			release = holdMigrationLock(t, pool)
		}
		return len(p), nil
	}), nil))

	fsys := fstest.MapFS{"001_probe.sql": {Data: []byte("-- +goose Up\nCREATE TABLE version_log_probe (id INT);\n")}}
	timeouts := migrationTimeouts{total: 3 * time.Second, lock: 150 * time.Millisecond, unlock: time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := applyWithTimeouts(ctx, pool, fsys, logger, timeouts); err != nil {
		t.Fatalf("version read waited for a second lock acquisition: %v", err)
	}
	if release == nil {
		t.Fatal("test did not acquire the competing lock after migration")
	}
	defer release()
	// An up-to-date replica only needs an unlocked version read, even while
	// another replica holds the lock for its own pending work.
	if err := applyWithTimeouts(ctx, pool, fsys, logger, timeouts); err != nil {
		t.Fatalf("no-op startup waited for the migration lock: %v", err)
	}
	if got := appliedVersions(t, pool); len(got) != 1 || got[0] != 1 {
		t.Fatalf("versions = %v, want [1]", got)
	}
}
