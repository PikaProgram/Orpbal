// Package observability hosts the bonus B2 wiring: Prometheus metrics for the
// queue/worker/DB pool and an OTLP log writer. Tracing lives in internal/tracing
// and is reused here so the API → queue → worker trace stays one trace.
package observability

import (
	"context"
	"database/sql"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	jobClaims = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orpbal_job_claims_total",
		Help: "Migration job claim attempts by result.",
	}, []string{"result"})

	jobRetries = promauto.NewCounter(prometheus.CounterOpts{
		Name: "orpbal_job_retries_total",
		Help: "Transient migration job failures scheduled for retry.",
	})

	jobFinishes = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orpbal_job_finishes_total",
		Help: "Migration jobs reaching a terminal status.",
	}, []string{"status"})

	queueWait = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "orpbal_job_queue_wait_seconds",
		Help:    "Time between job creation and worker claim.",
		Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300, 600},
	})

	lockWait = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "orpbal_job_lock_wait_seconds",
		Help:    "Time spent acquiring the per-database advisory lock.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10, 30},
	})

	migrationDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "orpbal_migration_duration_seconds",
		Help:    "End-to-end worker duration of one migration execution.",
		Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300, 600},
	}, []string{"kind"})

	batchDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "orpbal_batch_duration_seconds",
		Help:    "Duration of one committed migration batch.",
		Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10, 30, 60},
	})

	batches = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orpbal_batches_total",
		Help: "Committed migration batches by execution kind.",
	}, []string{"kind"})

	rows = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orpbal_rows_total",
		Help: "Rows affected by committed migration batches by execution kind.",
	}, []string{"kind"})
)

// JobClaimed records claim outcomes and queue/lock wait times. Label values
// come from bounded enums/results — never job IDs, user IDs, or SQL text.
func JobClaimed(result string, queuedFor, connWait, advisoryWait time.Duration) {
	jobClaims.WithLabelValues(result).Inc()
	if result == "claimed" {
		queueWait.Observe(queuedFor.Seconds())
		lockWait.Observe(connWait.Seconds() + advisoryWait.Seconds())
	}
}

// JobRetried records a transient failure that returned a job to the queue.
func JobRetried() { jobRetries.Inc() }

// JobFinished records a terminal job status.
func JobFinished(status string) { jobFinishes.WithLabelValues(status).Inc() }

// MigrationFinished records the duration of a worker execution.
func MigrationFinished(kind string, elapsed time.Duration) {
	migrationDuration.WithLabelValues(kind).Observe(elapsed.Seconds())
}

// BatchCommitted records one committed batch and its row count.
func BatchCommitted(kind string, affected int64, elapsed time.Duration) {
	batches.WithLabelValues(kind).Inc()
	rows.WithLabelValues(kind).Add(float64(affected))
	batchDuration.Observe(elapsed.Seconds())
}

// Handler exposes the default Prometheus registry (including Go runtime
// metrics). It is mounted at /metrics on the API and on the worker.
func Handler() http.Handler { return promhttp.Handler() }

// dbCollector exposes database/sql pool stats and the current job status
// counts. Both are read at scrape time so no queue hot path is needed for
// status counts.
type dbCollector struct {
	mu    sync.RWMutex
	pools map[string]*sql.DB
	jobs  *sql.DB
}

var (
	descConnections = prometheus.NewDesc(
		"orpbal_db_pool_connections",
		"Platform database connections by state (open, idle, in_use, max_open).",
		[]string{"pool", "state"}, nil)
	descWaitSeconds = prometheus.NewDesc(
		"orpbal_db_pool_wait_seconds_total",
		"Cumulative time waiting for a database connection.",
		[]string{"pool"}, nil)
	descWaitCount = prometheus.NewDesc(
		"orpbal_db_pool_wait_count_total",
		"Cumulative number of waits for a database connection.",
		[]string{"pool"}, nil)
	descJobStatus = prometheus.NewDesc(
		"orpbal_jobs",
		"Current migration jobs by status.",
		[]string{"status"}, nil)

	registeredDBs = &dbCollector{pools: map[string]*sql.DB{}}
)

func init() { prometheus.MustRegister(registeredDBs) }

// RegisterDB attaches a *sql.DB to the metrics endpoint. withJobs enables the
// scrape-time migration_jobs status query (only needed once per platform DB).
func RegisterDB(db *sql.DB, pool string, withJobs bool) {
	if db == nil {
		return
	}
	registeredDBs.mu.Lock()
	registeredDBs.pools[pool] = db
	if withJobs {
		registeredDBs.jobs = db
	}
	registeredDBs.mu.Unlock()
}

func (c *dbCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descConnections
	ch <- descWaitSeconds
	ch <- descWaitCount
	ch <- descJobStatus
}

func (c *dbCollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.RLock()
	pools := make(map[string]*sql.DB, len(c.pools))
	for name, db := range c.pools {
		pools[name] = db
	}
	jobs := c.jobs
	c.mu.RUnlock()

	for name, db := range pools {
		stats := db.Stats()
		ch <- prometheus.MustNewConstMetric(descConnections, prometheus.GaugeValue, float64(stats.OpenConnections), name, "open")
		ch <- prometheus.MustNewConstMetric(descConnections, prometheus.GaugeValue, float64(stats.Idle), name, "idle")
		ch <- prometheus.MustNewConstMetric(descConnections, prometheus.GaugeValue, float64(stats.InUse), name, "in_use")
		ch <- prometheus.MustNewConstMetric(descConnections, prometheus.GaugeValue, float64(stats.MaxOpenConnections), name, "max_open")
		ch <- prometheus.MustNewConstMetric(descWaitSeconds, prometheus.CounterValue, stats.WaitDuration.Seconds(), name)
		ch <- prometheus.MustNewConstMetric(descWaitCount, prometheus.CounterValue, float64(stats.WaitCount), name)
	}

	if jobs == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := jobs.QueryContext(ctx, "SELECT status, count(*) FROM migration_jobs GROUP BY status")
	if err != nil {
		ch <- prometheus.NewInvalidMetric(descJobStatus, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			ch <- prometheus.NewInvalidMetric(descJobStatus, err)
			return
		}
		ch <- prometheus.MustNewConstMetric(descJobStatus, prometheus.GaugeValue, float64(count), status)
	}
	if err := rows.Err(); err != nil {
		ch <- prometheus.NewInvalidMetric(descJobStatus, err)
	}
}
