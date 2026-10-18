package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

const (
	// readyTimeout bounds the database ping so a hung connection fails the
	// check instead of stalling the orchestrator's probe.
	readyTimeout = 2 * time.Second
	// readyCacheTTL lets a burst of unauthenticated /ready requests share one
	// ping instead of each taking a connection from the application's pool.
	readyCacheTTL = time.Second
)

// Pinger reports whether a dependency is reachable. *pgxpool.Pool satisfies it.
type Pinger interface {
	Ping(ctx context.Context) error
}

// ReadyHandler reports whether the app's dependencies are reachable.
type ReadyHandler struct {
	db  Pinger
	now func() time.Time

	// mu allows one ping at a time; concurrent requests wait and then reuse
	// its result while it is fresher than readyCacheTTL.
	mu        sync.Mutex
	checkedAt time.Time
	dbOK      bool
}

// NewReadyHandler creates a new ReadyHandler
func NewReadyHandler(db Pinger) *ReadyHandler {
	return &ReadyHandler{db: db, now: time.Now}
}

type readyResponse struct {
	Status string `json:"status"`
	DB     string `json:"db"`
}

// Ready pings the database and returns 200 when it answers, 503 otherwise.
// The response never includes the error text, which may name hosts or users.
func (h *ReadyHandler) Ready(w http.ResponseWriter, r *http.Request) {
	status, resp := http.StatusOK, readyResponse{Status: "ok", DB: "ok"}
	if !h.dbReady(r.Context()) {
		status, resp = http.StatusServiceUnavailable, readyResponse{Status: "degraded", DB: "error"}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *ReadyHandler) dbReady(ctx context.Context) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.checkedAt.IsZero() && h.now().Sub(h.checkedAt) < readyCacheTTL {
		return h.dbOK
	}

	// The result is shared, so one client disconnecting must not cancel the
	// ping and report the database as down to everyone else.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), readyTimeout)
	defer cancel()

	err := h.db.Ping(ctx)
	if err != nil {
		slog.Warn("readiness check failed", "dependency", "db", "error", err)
	}
	h.checkedAt, h.dbOK = h.now(), err == nil
	return h.dbOK
}
