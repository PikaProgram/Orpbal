// Package tracing wires OpenTelemetry tracing with a stdout (console)
// exporter and zerolog correlation: trace_id/span_id land on every log line,
// and W3C traceparent strings travel API → queue → worker via the job row.
package tracing

import (
	"context"
	"sync"

	"github.com/rs/zerolog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

var (
	tracer   trace.Tracer
	once     sync.Once
	shutdown func(context.Context) error = func(context.Context) error { return nil }
)

// Init installs the stdout-exporting tracer provider. Safe to call once;
// later calls are no-ops. Spans print as JSON to stdout.
func Init(serviceName string) (func(context.Context) error, error) {
	var err error
	once.Do(func() {
		exp, expErr := stdouttrace.New()
		if expErr != nil {
			err = expErr
			return
		}
		res, resErr := resource.Merge(resource.Default(),
			resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(serviceName)))
		if resErr != nil {
			res = resource.Default()
		}
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(exp),
			sdktrace.WithResource(res),
		)
		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(propagation.TraceContext{})
		tracer = tp.Tracer(serviceName)
		shutdown = tp.Shutdown
	})
	return shutdown, err
}

// Tracer returns the package tracer; nil before Init.
func Tracer() trace.Tracer { return tracer }

// StartSpan starts a span named event, resuming from a W3C traceparent string
// when one is present (worker resuming a job's API-initiated trace).
func StartSpan(ctx context.Context, traceparent, event string) (context.Context, trace.Span) {
	if traceparent != "" {
		if c, ok := resume(ctx, traceparent); ok {
			ctx = c
		}
	}
	if tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	ctx, span := tracer.Start(ctx, event)
	return ctx, span
}

// TraceLogger returns a logger whose events carry trace_id/span_id from ctx.
// Usage in worker/handler code:
//
//	log := tracing.TraceLogger(logger, ctx).Str("event", "batch_commit").Msg("...")
type traceLoggerKey struct{}

// WithLogCtx stores ctx so TraceLogger can read it back.
func WithLogCtx(ctx context.Context) context.Context { return context.WithValue(ctx, traceLoggerKey{}, ctx) }

func TraceLogger(base zerolog.Logger, ctx context.Context) zerolog.Logger {
	span := trace.SpanFromContext(ctx)
	sc := span.SpanContext()
	if !sc.IsValid() {
		return base
	}
	return base.With().Str("trace_id", sc.TraceID().String()).Str("span_id", sc.SpanID().String()).Logger()
}

// TraceparentOf renders the current span as a W3C traceparent string, or ""
// when there is no valid span. Stored on the job row at submission; the
// worker uses it to resume the same trace.
func TraceparentOf(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String() + "-" + sc.SpanID().String()
}

// ParseTraceparent renders a stored traceparent into a HeaderCarrier extract
// call — the canonical W3C form "00-<trace_id>-<span_id>-01" is what we store
// via SpanContext, so keep the raw W3C string instead.
func resume(ctx context.Context, traceparent string) (context.Context, bool) {
	carrier := propagation.HeaderCarrier{}
	carrier.Set("traceparent", traceparent)
	ctx = otel.GetTextMapPropagator().Extract(ctx, carrier)
	return ctx, trace.SpanContextFromContext(ctx).IsValid()
}

// W3CTraceparent renders the canonical W3C traceparent header value for the
// span in ctx; "" when no valid span. This is the value stored on the job row.
func W3CTraceparent(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	carrier := propagation.HeaderCarrier{}
	propagation.TraceContext{}.Inject(trace.ContextWithSpanContext(ctx, sc), carrier)
	return carrier.Get("traceparent")
}
