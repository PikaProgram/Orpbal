package observability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"Orpbal/internal/tracing"
)

// Runtime owns the telemetry providers started for one binary.
type Runtime struct {
	// LogWriter is the io.Writer to hand to zerolog. It preserves local logs
	// and mirrors them to the collector when OTLP is configured.
	LogWriter io.Writer

	shutdowns []func(context.Context) error
}

// ServiceName resolves OTEL_SERVICE_NAME with a binary-specific default.
func ServiceName(fallback string) string {
	if name := os.Getenv("OTEL_SERVICE_NAME"); name != "" {
		return name
	}
	return fallback
}

// Init sets up tracing and the OTLP log bridge. It does not start the metrics
// endpoint: the API mounts Handler() on its router, the worker calls
// StartMetricsServer for its own port.
func Init(serviceName string, base io.Writer) (*Runtime, error) {
	rt := &Runtime{}

	shutdownTracing, err := tracing.Init(serviceName)
	if err != nil {
		return nil, fmt.Errorf("tracing init: %w", err)
	}
	rt.shutdowns = append(rt.shutdowns, shutdownTracing)

	writer, shutdownLogs, err := NewLogWriter(base, serviceName)
	if err != nil {
		return nil, err
	}
	rt.LogWriter = writer
	rt.shutdowns = append(rt.shutdowns, shutdownLogs)
	return rt, nil
}

// Shutdown flushes pending spans/logs. Call before process exit.
func (r *Runtime) Shutdown(ctx context.Context) error {
	var errs []error
	for i := len(r.shutdowns) - 1; i >= 0; i-- {
		if err := r.shutdowns[i](ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// StartMetricsServer serves /metrics on addr until ctx is cancelled. Binding
// happens synchronously so startup errors are visible to the caller.
func StartMetricsServer(ctx context.Context, addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(listener) }()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	return nil
}
