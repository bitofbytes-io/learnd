package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/drywaters/learnd/internal/config"
	"github.com/drywaters/learnd/internal/metrics"
)

// /metrics lives only on the internal listener. The public router must not
// serve it, or a public Traefik route could expose it.
func TestPublicRouterDoesNotServeMetrics(t *testing.T) {
	cfg := &config.Config{APIToken: testAPIToken, SecureCookies: true, Location: time.UTC}
	router := New(cfg, nil, pingerFunc(func(context.Context) error { return nil }), false).Router()

	for _, auth := range []string{"", "Bearer " + testAPIToken} {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET /metrics (auth %q) on the public router = %d, want 404", auth, rec.Code)
		}
	}
}

// The real router labels requests by chi route pattern and counts rejected
// credentials, and both show up on the metrics handler.
func TestRouterRecordsREDAndLoginFailures(t *testing.T) {
	cfg := &config.Config{APIToken: testAPIToken, SecureCookies: true, Location: time.UTC}
	router := New(cfg, nil, pingerFunc(func(context.Context) error { return nil }), false).Router()

	const entryID = "7d1c3b0e-5f0a-4c1e-9a55-2f6f3c1d8e42"
	requests := []*http.Request{
		// Unauthenticated: redirected to /login, but still matched a route.
		httptest.NewRequest(http.MethodGet, "/entries/"+entryID+"/edit?return_to=%2F", nil),
		httptest.NewRequest(http.MethodGet, "/health", nil),
	}
	badBearer := httptest.NewRequest(http.MethodPost, "/api/entries/"+entryID+"/refresh-summary", nil)
	badBearer.Header.Set("Authorization", "Bearer wrong")
	badLogin := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(url.Values{"api_key": {"wrong"}}.Encode()))
	badLogin.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	badLogin.Header.Set("Origin", "http://example.com")
	requests = append(requests, badBearer, badLogin)
	for _, req := range requests {
		router.ServeHTTP(httptest.NewRecorder(), req)
	}

	srv := httptest.NewServer(metrics.Handler(metrics.NewRegistry()))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)

	for _, want := range []string{
		`http_requests_total{code="303",method="GET",route="/entries/{id}/edit"} `,
		`http_requests_total{code="401",method="POST",route="/api/entries/{id}/refresh-summary"} `,
		`http_requests_total{code="303",method="POST",route="/login"} `,
		`http_request_duration_seconds_count{code="303",method="GET",route="/entries/{id}/edit"} `,
		`learnd_login_failures_total{reason="invalid_key"} `,
		`learnd_login_failures_total{reason="invalid_bearer_token"} `,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output is missing %s", want)
		}
	}
	for _, leaked := range []string{entryID, "return_to", `route="/health"`} {
		if strings.Contains(body, leaked) {
			t.Errorf("metrics output contains %q; routes must be labelled by pattern", leaked)
		}
	}
	if strings.Contains(body, `learnd_login_failures_total{reason="invalid_key"} 0`) {
		t.Error("invalid login was not counted")
	}
}
