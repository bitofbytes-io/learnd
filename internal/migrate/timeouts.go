package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/pressly/goose/v3/lock"
)

type migrationTimeouts struct {
	total, lock, unlock time.Duration
}

// Keep these budgets aligned in Dined and Learnd and below the deployment's
// 90-second healthcheck start period: connect 10s + migrate 60s + cleanup 5s.
var startupTimeouts = migrationTimeouts{
	total:  60 * time.Second,
	lock:   30 * time.Second,
	unlock: 5 * time.Second,
}

type deadlineLocker struct {
	locker   lock.SessionLocker
	timeouts migrationTimeouts
}

func (l deadlineLocker) SessionLock(ctx context.Context, conn *sql.Conn) error {
	ctx, cancel := context.WithTimeout(ctx, l.timeouts.lock)
	defer cancel()
	if err := l.locker.SessionLock(ctx, conn); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("migration lock wait interrupted: %w", ctx.Err())
		}
		return err
	}
	return nil
}

func (l deadlineLocker) SessionUnlock(ctx context.Context, conn *sql.Conn) error {
	// Goose detaches its cleanup context. Add an explicit deadline so a failed
	// unlock cannot extend startup by its default one-minute retry budget.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), l.timeouts.unlock)
	defer cancel()
	return l.locker.SessionUnlock(ctx, conn)
}
