package metrics

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
)

func scrape(t *testing.T, reg *prometheus.Registry) string {
	t.Helper()
	srv := httptest.NewServer(Handler(reg))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /metrics = %d: %s", resp.StatusCode, body)
	}
	return string(body)
}

func assertContains(t *testing.T, body string, series ...string) {
	t.Helper()
	for _, s := range series {
		if !strings.Contains(body, s) {
			t.Errorf("metrics output is missing %s", s)
		}
	}
}

func TestMiddlewareLabelsByRoutePattern(t *testing.T) {
	r := chi.NewRouter()
	r.Use(Middleware)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {})
	r.Get("/ready", func(w http.ResponseWriter, r *http.Request) {})
	r.Get("/red-test/{id}", func(w http.ResponseWriter, r *http.Request) {})
	r.Group(func(r chi.Router) {
		r.Post("/red-test/{id}/refresh", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "nope", http.StatusTeapot)
		})
	})

	// The counters are process-wide, so compare increases rather than totals.
	want := map[[3]string]float64{
		{"GET", "/red-test/{id}", "200"}:          2,
		{"POST", "/red-test/{id}/refresh", "418"}: 1,
		{"GET", unmatchedRoute, "404"}:            1,
		{"OTHER", unmatchedRoute, "405"}:          1,
	}
	before := map[[3]string]float64{}
	for series := range want {
		before[series] = testutil.ToFloat64(httpRequests.WithLabelValues(series[:]...))
	}

	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/red-test/41?user=alice", nil),
		httptest.NewRequest(http.MethodGet, "/red-test/42", nil),
		httptest.NewRequest(http.MethodPost, "/red-test/42/refresh", nil),
		httptest.NewRequest(http.MethodGet, "/no-such-page/9f2c", nil),
		httptest.NewRequest("BREW", "/red-test/43", nil),
		httptest.NewRequest(http.MethodGet, "/health", nil),
		httptest.NewRequest(http.MethodGet, "/ready", nil),
	} {
		r.ServeHTTP(httptest.NewRecorder(), req)
	}

	for series, rise := range want {
		if got := testutil.ToFloat64(httpRequests.WithLabelValues(series[:]...)) - before[series]; got != rise {
			t.Errorf("http_requests_total%v rose by %v, want %v", series, got, rise)
		}
	}
	body := scrape(t, NewRegistry())
	assertContains(t, body,
		`http_requests_total{code="200",method="GET",route="/red-test/{id}"} `,
		`http_request_duration_seconds_bucket{code="200",method="GET",route="/red-test/{id}",le="+Inf"} `,
		`http_request_duration_seconds_count{code="418",method="POST",route="/red-test/{id}/refresh"} `,
	)
	for _, leaked := range []string{"/red-test/41", "/red-test/42", "user=alice", "/no-such-page", `route="/health"`, `route="/ready"`, `method="BREW"`} {
		if strings.Contains(body, leaked) {
			t.Errorf("metrics output contains %q; routes must be labelled by pattern", leaked)
		}
	}
}

// Middleware runs before Recoverer on the app router, so a recovered panic
// must be counted as a 500 on its route, not as a 200.
func TestMiddlewareCountsRecoveredPanicAs500(t *testing.T) {
	r := chi.NewRouter()
	r.Use(Middleware)
	r.Use(chimw.Recoverer)
	r.Get("/panic-test/{id}", func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})

	series := []string{"GET", "/panic-test/{id}", "500"}
	requestsBefore := testutil.ToFloat64(httpRequests.WithLabelValues(series...))
	durationsBefore := histogramCount(t, series)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panic-test/7", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}

	if got := testutil.ToFloat64(httpRequests.WithLabelValues(series...)) - requestsBefore; got != 1 {
		t.Errorf("http_requests_total%v rose by %v, want 1", series, got)
	}
	if got := histogramCount(t, series) - durationsBefore; got != 1 {
		t.Errorf("http_request_duration_seconds_count%v rose by %v, want 1", series, got)
	}
}

func histogramCount(t *testing.T, labels []string) uint64 {
	t.Helper()
	var m dto.Metric
	if err := httpDuration.WithLabelValues(labels...).(prometheus.Metric).Write(&m); err != nil {
		t.Fatal(err)
	}
	return m.GetHistogram().GetSampleCount()
}

func TestRegistryExportsDomainAndRuntimeSeries(t *testing.T) {
	counters := []struct {
		counter prometheus.Counter
		record  func()
	}{
		{loginFailures.WithLabelValues(ReasonInvalidKey), func() { LoginFailure(ReasonInvalidKey) }},
		{jobs.WithLabelValues(JobEnrichment, ResultSuccess), func() { Job(JobEnrichment, ResultSuccess) }},
		{jobs.WithLabelValues(JobSummary, ResultSkipped), func() { Job(JobSummary, ResultSkipped) }},
	}
	for i, c := range counters {
		before := testutil.ToFloat64(c.counter)
		c.record()
		if got := testutil.ToFloat64(c.counter) - before; got != 1 {
			t.Errorf("counter %d rose by %v, want 1", i, got)
		}
	}

	// Every known series is exported, including ones still at zero.
	body := scrape(t, NewRegistry())
	assertContains(t, body,
		`learnd_login_failures_total{reason="invalid_request"} `,
		`learnd_login_failures_total{reason="missing_key"} `,
		`learnd_login_failures_total{reason="invalid_key"} `,
		`learnd_login_failures_total{reason="invalid_bearer_token"} `,
		`learnd_login_failures_total{reason="invalid_cookie"} `,
		`learnd_jobs_total{job="enrichment",result="success"} `,
		`learnd_jobs_total{job="enrichment",result="failure"} `,
		`learnd_jobs_total{job="summary",result="success"} `,
		`learnd_jobs_total{job="summary",result="failure"} `,
		`learnd_jobs_total{job="summary",result="skipped"} `,
		"go_goroutines ",
		"process_start_time_seconds ",
	)
}

func TestPoolCollectorExportsPoolStats(t *testing.T) {
	// pgxpool connects lazily, so an unreachable address still yields stats.
	pool, err := pgxpool.New(context.Background(), "postgres://learnd:learnd@127.0.0.1:1/learnd?pool_max_conns=7")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	body := scrape(t, NewRegistry(NewPoolCollector(pool)))
	assertContains(t, body,
		"pgxpool_max_conns 7",
		"pgxpool_acquired_conns 0",
		"pgxpool_idle_conns 0",
		"pgxpool_total_conns 0",
		"pgxpool_acquires_total 0",
		"pgxpool_empty_acquire_wait_seconds_total 0",
		"# TYPE pgxpool_acquire_duration_seconds_total counter",
	)
}

func TestHandlerServesOnlyMetrics(t *testing.T) {
	srv := httptest.NewServer(Handler(NewRegistry()))
	defer srv.Close()

	for path, want := range map[string]int{"/": http.StatusNotFound, "/health": http.StatusNotFound, "/metrics/x": http.StatusNotFound} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("GET %s = %d, want %d", path, resp.StatusCode, want)
		}
	}
	resp, err := http.Post(srv.URL+"/metrics", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /metrics = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestStartServesMetricsAndShutsDown(t *testing.T) {
	srv := Start("127.0.0.1:0", NewRegistry())
	if srv == nil {
		t.Fatal("Start returned nil for a free address")
	}
	url := "http://" + srv.Addr().String() + "/metrics"
	resp, err := http.Get(url) // #nosec G107 -- URL is the loopback test listener created above.
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "learnd_jobs_total") {
		t.Fatalf("GET /metrics = %d: %.200s", resp.StatusCode, body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if resp, err := http.Get(url); err == nil { // #nosec G107 -- probes the same loopback test listener after shutdown.
		_ = resp.Body.Close()
		t.Fatal("metrics listener still serving after Shutdown")
	}
}

func TestStartIsOptional(t *testing.T) {
	if srv := Start(Disabled, NewRegistry()); srv != nil {
		t.Fatal("METRICS_ADDR=off should not start a listener")
	}

	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = taken.Close() }()
	srv := Start(taken.Addr().String(), NewRegistry())
	if srv != nil {
		t.Fatal("Start should return nil when the address is in use")
	}
	// A nil server shuts down cleanly, so callers need no special case.
	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}
