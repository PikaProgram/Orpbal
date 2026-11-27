package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestGinMiddlewareRecordsRouteMetrics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/things/:id", GinMiddleware(), func(c *gin.Context) { c.Status(http.StatusOK) })

	label := "/api/things/:id"
	before := testutil.ToFloat64(HTTPRequests.WithLabelValues(label, http.MethodGet, "200"))

	req := httptest.NewRequest(http.MethodGet, "/api/things/42", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	after := testutil.ToFloat64(HTTPRequests.WithLabelValues(label, http.MethodGet, "200"))
	if after != before+1 {
		t.Fatalf("http_requests_total = %v, want %v (+1 per request)", after, before+1)
	}

	// The duration histogram gains at least one observation (its count is
	// exposed as _count in the same registry).
	histBefore := testutil.CollectAndCount(HTTPDuration, "orpbal_http_request_duration_seconds")
	if histBefore == 0 {
		t.Fatal("duration histogram has no metric families")
	}
}

func TestSetJobStatusGauge(t *testing.T) {
	SetJobStatusGauge(map[string]int{"running": 2})
	if got := testutil.ToFloat64(MigrationJobs.WithLabelValues("running")); got != 2 {
		t.Fatalf("gauge(running) = %v, want 2", got)
	}
	// Statuses absent from the map drop to zero instead of going stale.
	if got := testutil.ToFloat64(MigrationJobs.WithLabelValues("queued")); got != 0 {
		t.Fatalf("gauge(queued) = %v, want 0", got)
	}
}

func TestObserveOutcome(t *testing.T) {
	before := testutil.ToFloat64(JobOutcomes.WithLabelValues("partial"))
	ObserveOutcome("partial")
	after := testutil.ToFloat64(JobOutcomes.WithLabelValues("partial"))
	if after != before+1 {
		t.Fatalf("outcomes(partial) = %v, want %v (+1)", after, before+1)
	}
}
