package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/pressly/goose/v3/lock"
)

// migrationTimeouts separates the overall startup deadline from lock and cleanup limits.
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

// deadlineLocker bounds Goose lock queries and its detached cleanup context.
type deadlineLocker struct {
	locker   lock.SessionLocker
	timeouts migrationTimeouts
}

// SessionLock limits acquisition to the smaller of the startup and lock budgets.
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

// SessionUnlock allows cleanup after cancellation, but only for its own bounded budget.
func (l deadlineLocker) SessionUnlock(ctx context.Context, conn *sql.Conn) error {
	// Goose detaches its cleanup context. Add an explicit deadline so a failed
	// unlock cannot extend startup by its default one-minute retry budget.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), l.timeouts.unlock)
	defer cancel()
	return l.locker.SessionUnlock(ctx, conn)
}

// retryCount rounds a budget up to one-second retry intervals, with at least
// one retry as required by Goose. The context deadline remains authoritative;
// rounding up prevents the retry limit from ending a fractional budget early.
func retryCount(budget time.Duration) uint64 {
	if budget <= 0 {
		return 1
	}
	count := budget / time.Second
	if budget%time.Second != 0 {
		count++
	}
	return uint64(count)
}
