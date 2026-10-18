// Package metrics exposes Prometheus metrics for learnd on a separate internal
// listener, never on the public router. It owns its registry instead of using
// the global default, so only the collectors registered here are exported.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Result label values for learnd_jobs_total.
const (
	ResultSuccess = "success"
	ResultFailure = "failure"
	ResultSkipped = "skipped"
)

// Job label values for the background worker's job kinds.
const (
	JobEnrichment = "enrichment"
	JobSummary    = "summary"
)

// Reason label values for learnd_login_failures_total. They match the reasons
// already written to the auth logs.
const (
	ReasonInvalidRequest     = "invalid_request"
	ReasonMissingKey         = "missing_key"
	ReasonInvalidKey         = "invalid_key"
	ReasonInvalidBearerToken = "invalid_bearer_token"
	ReasonInvalidCookie      = "invalid_cookie"
)

var (
	loginFailures = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "learnd_login_failures_total",
		Help: "Rejected credentials: failed login form submissions, invalid bearer tokens and invalid session cookies.",
	}, []string{"reason"})
	jobs = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "learnd_jobs_total",
		Help: "Background enrichment and summary jobs by job and result.",
	}, []string{"job", "result"})
)

func init() {
	// Start every known series at zero so rate() and alerts see it before the
	// first event.
	for _, reason := range []string{ReasonInvalidRequest, ReasonMissingKey, ReasonInvalidKey, ReasonInvalidBearerToken, ReasonInvalidCookie} {
		loginFailures.WithLabelValues(reason)
	}
	for _, job := range []string{JobEnrichment, JobSummary} {
		for _, result := range []string{ResultSuccess, ResultFailure} {
			jobs.WithLabelValues(job, result)
		}
	}
	jobs.WithLabelValues(JobSummary, ResultSkipped)
}

// NewRegistry returns a registry holding the Go runtime and process
// collectors, the HTTP RED metrics, the domain counters and any extra
// collectors, such as the database pool collector.
func NewRegistry(extra ...prometheus.Collector) *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		httpRequests,
		httpDuration,
		loginFailures,
		jobs,
	)
	reg.MustRegister(extra...)
	return reg
}

// Handler serves GET /metrics from reg. Every other path returns 404.
func Handler(reg *prometheus.Registry) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))
	return mux
}

// LoginFailure counts rejected credentials with one of the Reason constants.
func LoginFailure(reason string) {
	loginFailures.WithLabelValues(reason).Inc()
}

// Job counts one finished job with a Job constant and a Result constant.
func Job(job, result string) {
	jobs.WithLabelValues(job, result).Inc()
}
