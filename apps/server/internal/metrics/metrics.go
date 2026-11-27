// Package metrics wires Prometheus instrumentation into the platform: HTTP
// request metrics via a gin middleware, migration-queue metrics from the
// queue package, batch/attempt metrics from the worker, and a gauge of job
// states refreshed from the platform database. Exposed through
// promhttp.Handler() at /metrics (server) and the worker metrics port.
package metrics

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
)

var (
	// HTTPRequests counts API requests by route template, method, status.
	HTTPRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "orpbal",
		Name:      "http_requests_total",
		Help:      "API requests by route, method and HTTP status code.",
	}, []string{"route", "method", "status"})

	// HTTPDuration observes API request latency by route and method.
	HTTPDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "orpbal",
		Name:      "http_request_duration_seconds",
		Help:      "API request latency in seconds by route and method.",
		Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
	}, []string{"route", "method"})

	// MigrationJobs is refreshed from the platform DB (job states gauge).
	MigrationJobs = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "orpbal",
		Name:      "migration_jobs",
		Help:      "Migration jobs by queue status (refreshed periodically from migration_jobs).",
	}, []string{"status"})

	// JobsClaimed counts queue claims per worker.
	JobsClaimed = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "orpbal",
		Name:      "migration_jobs_claimed_total",
		Help:      "Migration jobs claimed from the queue per worker.",
	}, []string{"worker"})

	// JobOutcomes counts terminal outcomes and retries per status.
	JobOutcomes = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "orpbal",
		Name:      "migration_job_outcomes_total",
		Help:      "Migration job terminal outcomes and retries (success, failed, retry, cancelled, partial).",
	}, []string{"status"})

	// BatchesCommitted counts committed migration batches.
	BatchesCommitted = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "orpbal",
		Name:      "migration_batches_committed_total",
		Help:      "Migration batches committed against targets.",
	})

	// BatchRows counts rows processed by committed batches.
	BatchRows = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "orpbal",
		Name:      "migration_batch_rows_total",
		Help:      "Rows processed by committed migration batches.",
	})

	// Attempts counts append-only attempt rows by execution kind.
	Attempts = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "orpbal",
		Name:      "migration_attempts_total",
		Help:      "Migration attempt rows recorded (append-only history) by execution kind.",
	}, []string{"kind"})
)

// jobStatuses enumerates every migration_jobs status so the gauge drops to
// zero for empty states instead of disappearing.
var jobStatuses = []string{"queued", "running", "success", "failed", "cancelled", "cancelling", "partial"}

// Handler returns the /metrics HTTP handler (Prometheus text format).
func Handler() http.Handler { return promhttp.Handler() }

// GinMiddleware records request count and latency by route template.
func GinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		status := strconv.Itoa(c.Writer.Status())
		HTTPRequests.WithLabelValues(route, c.Request.Method, status).Inc()
		HTTPDuration.WithLabelValues(route, c.Request.Method).Observe(time.Since(start).Seconds())
	}
}

// SetJobStatusGauge refreshes the per-status job gauge from DB counts.
func SetJobStatusGauge(counts map[string]int) {
	for _, status := range jobStatuses {
		MigrationJobs.WithLabelValues(status).Set(float64(counts[status]))
	}
}

// ObserveOutcome records one job outcome (also used for retries).
func ObserveOutcome(status string) {
	JobOutcomes.WithLabelValues(status).Inc()
}
