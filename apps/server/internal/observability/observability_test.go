package observability

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestScrubTextRedactsCredentials(t *testing.T) {
	line := `{"level":"info","dsn":"postgres://user:password@db:5432/app","message":"connect password=hunter2 token=abc"}`
	got := scrubText(line)
	if strings.Contains(got, "hunter2") || strings.Contains(got, "token=abc") {
		t.Fatalf("credentials leaked after scrub: %s", got)
	}
	if !strings.Contains(got, "password=[REDACTED]") {
		t.Fatalf("expected redaction marker, got %s", got)
	}
}

func TestLogWriterPassThroughWithoutEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "")

	var buf bytes.Buffer
	w, shutdown, err := NewLogWriter(&buf, "test")
	if err != nil {
		t.Fatal(err)
	}
	line := "{\"level\":\"info\",\"message\":\"hello\"}\n"
	if _, err := w.Write([]byte(line)); err != nil {
		t.Fatal(err)
	}
	if buf.String() != line {
		t.Fatalf("log was not preserved: %q", buf.String())
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestJSONLogsFollowsLogLevelAndEndpoint(t *testing.T) {
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "")
	if JSONLogs() {
		t.Fatal("expected console logs without production level or OTLP endpoint")
	}
	t.Setenv("LOG_LEVEL", "production")
	if !JSONLogs() {
		t.Fatal("expected JSON logs in production")
	}
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "http://collector:4318")
	if !JSONLogs() {
		t.Fatal("expected JSON logs with OTLP logs endpoint")
	}
}

func TestMetricsRecordQueueAndBatchOutcomes(t *testing.T) {
	beforeClaims := testutil.ToFloat64(jobClaims.WithLabelValues("claimed"))
	beforeRetries := testutil.ToFloat64(jobRetries)
	beforeBatches := testutil.ToFloat64(batches.WithLabelValues("up"))
	beforeRows := testutil.ToFloat64(rows.WithLabelValues("up"))

	JobClaimed("claimed", 250*time.Millisecond, 5*time.Millisecond, time.Millisecond)
	JobRetried()
	BatchCommitted("up", 1000, 10*time.Millisecond)

	if got := testutil.ToFloat64(jobClaims.WithLabelValues("claimed")); got != beforeClaims+1 {
		t.Fatalf("claims: got %v, want %v", got, beforeClaims+1)
	}
	if got := testutil.ToFloat64(jobRetries); got != beforeRetries+1 {
		t.Fatalf("retries: got %v, want %v", got, beforeRetries+1)
	}
	if got := testutil.ToFloat64(batches.WithLabelValues("up")); got != beforeBatches+1 {
		t.Fatalf("batches: got %v, want %v", got, beforeBatches+1)
	}
	if got := testutil.ToFloat64(rows.WithLabelValues("up")); got != beforeRows+1000 {
		t.Fatalf("rows: got %v, want %v", got, beforeRows+1000)
	}
}
