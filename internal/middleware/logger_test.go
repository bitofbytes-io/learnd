package middleware

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoggerEmitsDebugAccessLog(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantStatus string
	}{
		{
			name: "explicit successful response",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			},
			wantStatus: "status=204",
		},
		{
			name:       "implicit ok response",
			handler:    func(http.ResponseWriter, *http.Request) {},
			wantStatus: "status=200",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			})))
			t.Cleanup(func() {
				slog.SetDefault(previous)
			})

			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			rec := httptest.NewRecorder()

			Logger(tt.handler).ServeHTTP(rec, req)

			got := buf.String()
			for _, want := range []string{"level=DEBUG", "msg=\"http request\"", "method=GET", "path=/health", tt.wantStatus} {
				if !strings.Contains(got, want) {
					t.Fatalf("expected log to contain %q, got %q", want, got)
				}
			}
		})
	}
}

func TestLoggerElevatesClientAndServerErrors(t *testing.T) {
	tests := []struct {
		name      string
		code      int
		wantLevel string
	}{
		{
			name:      "client error",
			code:      http.StatusNotFound,
			wantLevel: "level=WARN",
		},
		{
			name:      "server error",
			code:      http.StatusInternalServerError,
			wantLevel: "level=ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
				Level: slog.LevelInfo,
			})))
			t.Cleanup(func() {
				slog.SetDefault(previous)
			})

			req := httptest.NewRequest(http.MethodGet, "/missing", nil)
			rec := httptest.NewRecorder()

			Logger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.code)
			})).ServeHTTP(rec, req)

			got := buf.String()
			for _, want := range []string{tt.wantLevel, "msg=\"http request\"", "method=GET", "path=/missing", fmt.Sprintf("status=%d", tt.code)} {
				if !strings.Contains(got, want) {
					t.Fatalf("expected log to contain %q, got %q", want, got)
				}
			}
		})
	}
}
