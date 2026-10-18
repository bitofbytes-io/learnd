package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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

// A burst of probes must not take one pool connection each: only one ping
// runs at a time, and requests within readyCacheTTL reuse its result.
func TestReadySharesOnePingAcrossConcurrentRequests(t *testing.T) {
	var calls, inFlight, maxInFlight atomic.Int32
	started, release := make(chan struct{}, 1), make(chan struct{})
	h := NewReadyHandler(pingerFunc(func(context.Context) error {
		calls.Add(1)
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		if n > maxInFlight.Load() {
			maxInFlight.Store(n)
		}
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		return nil
	}))
	now := time.Unix(1_700_000_000, 0)
	h.now = func() time.Time { return now }

	const requests = 20
	codes := make(chan int, requests)
	var wg sync.WaitGroup
	for range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			h.Ready(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
			codes <- rec.Code
		}()
	}
	<-started
	close(release)
	wg.Wait()
	close(codes)

	for code := range codes {
		if code != http.StatusOK {
			t.Fatalf("status = %d, want %d", code, http.StatusOK)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("pings = %d, want 1 for a burst within the cache TTL", got)
	}
	if got := maxInFlight.Load(); got != 1 {
		t.Fatalf("concurrent pings = %d, want 1", got)
	}

	now = now.Add(readyCacheTTL)
	h.Ready(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ready", nil))
	if got := calls.Load(); got != 2 {
		t.Fatalf("pings after TTL = %d, want 2", got)
	}
}

// One client hanging up must not cancel the shared ping and cache a failure.
func TestReadyIgnoresClientCancellation(t *testing.T) {
	h := NewReadyHandler(pingerFunc(func(ctx context.Context) error { return ctx.Err() }))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rec := httptest.NewRecorder()
	h.Ready(rec, httptest.NewRequest(http.MethodGet, "/ready", nil).WithContext(ctx))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
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
