package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// readyTimeout bounds the database ping so a hung connection fails the check
// instead of stalling the orchestrator's probe.
const readyTimeout = 2 * time.Second

// Pinger reports whether a dependency is reachable. *pgxpool.Pool satisfies it.
type Pinger interface {
	Ping(ctx context.Context) error
}

// ReadyHandler reports whether the app's dependencies are reachable.
type ReadyHandler struct {
	db Pinger
}

// NewReadyHandler creates a new ReadyHandler
func NewReadyHandler(db Pinger) *ReadyHandler {
	return &ReadyHandler{db: db}
}

type readyResponse struct {
	Status string `json:"status"`
	DB     string `json:"db"`
}

// Ready pings the database and returns 200 when it answers, 503 otherwise.
// The response never includes the error text, which may name hosts or users.
func (h *ReadyHandler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
	defer cancel()

	status, resp := http.StatusOK, readyResponse{Status: "ok", DB: "ok"}
	if err := h.db.Ping(ctx); err != nil {
		slog.Warn("readiness check failed", "dependency", "db", "error", err)
		status, resp = http.StatusServiceUnavailable, readyResponse{Status: "degraded", DB: "error"}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}
