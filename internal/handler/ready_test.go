package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type pingerFunc func(context.Context) error

func (f pingerFunc) Ping(ctx context.Context) error { return f(ctx) }

func TestReady(t *testing.T) {
	tests := []struct {
		name     string
		ping     pingerFunc
		wantCode int
		wantBody string
	}{
		{
			name:     "database up",
			ping:     func(context.Context) error { return nil },
			wantCode: http.StatusOK,
			wantBody: `{"status":"ok","db":"ok"}`,
		},
		{
			name: "database error",
			ping: func(context.Context) error {
				return errors.New("failed to connect to `user=learnd password=hunter2 host=db`")
			},
			wantCode: http.StatusServiceUnavailable,
			wantBody: `{"status":"degraded","db":"error"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			NewReadyHandler(tt.ping).Ready(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))

			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			if got := strings.TrimSpace(rec.Body.String()); got != tt.wantBody {
				t.Fatalf("body = %q, want %q", got, tt.wantBody)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", got)
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", got)
			}
		})
	}
}

func TestReadyBoundsPingWithTimeout(t *testing.T) {
	var deadline, called time.Time
	var hasDeadline bool
	timedOut := pingerFunc(func(ctx context.Context) error {
		called = time.Now()
		deadline, hasDeadline = ctx.Deadline()
		return context.DeadlineExceeded
	})

	start := time.Now()
	rec := httptest.NewRecorder()
	NewReadyHandler(timedOut).Ready(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if !hasDeadline {
		t.Fatal("ping context has no deadline")
	}
	if deadline.Before(start.Add(readyTimeout)) || deadline.After(called.Add(readyTimeout)) {
		t.Fatalf("ping deadline = %v, want %v after the request started", deadline.Sub(start), readyTimeout)
	}
}
