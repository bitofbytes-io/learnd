package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
)

// unmatchedRoute is the route label for requests no route pattern matched,
// so unknown paths cannot grow the label set.
const unmatchedRoute = "unmatched"

var (
	httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "HTTP requests by method, chi route pattern and status code.",
	}, []string{"method", "route", "code"})
	httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency by method, chi route pattern and status code.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route", "code"})
)

// skippedPaths are probe and scrape endpoints left out of the RED metrics.
var skippedPaths = map[string]bool{
	"/health":  true,
	"/ready":   true,
	"/metrics": true,
}

var knownMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodPost:    true,
	http.MethodPut:     true,
	http.MethodPatch:   true,
	http.MethodDelete:  true,
	http.MethodOptions: true,
	http.MethodConnect: true,
	http.MethodTrace:   true,
}

// Middleware records http_requests_total and http_request_duration_seconds.
// It must run inside a chi router so the matched route pattern, not the raw
// path, becomes the route label.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if skippedPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)

		defer func() {
			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}
			labels := prometheus.Labels{
				"method": methodLabel(r.Method),
				"route":  routeLabel(r),
				"code":   strconv.Itoa(status),
			}
			httpRequests.With(labels).Inc()
			httpDuration.With(labels).Observe(time.Since(start).Seconds())
		}()

		next.ServeHTTP(ww, r)
	})
}

func methodLabel(method string) string {
	if knownMethods[method] {
		return method
	}
	return "OTHER"
}

func routeLabel(r *http.Request) string {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil {
		return unmatchedRoute
	}
	if pattern := rctx.RoutePattern(); pattern != "" {
		return pattern
	}
	return unmatchedRoute
}
