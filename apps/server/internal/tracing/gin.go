package tracing

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// GinMiddleware opens a server span per request, resuming the W3C trace
// context from the incoming traceparent/tracestate headers so an external
// caller (or the SPA, or a demo client) can chain into the platform trace.
// The span-backed context replaces the request context, so every downstream
// consumer — service code, queue traceparent capture, sqllog — sees the same
// trace. Route templates (c.FullPath) keep span names low-cardinality.
func GinMiddleware(logger zerolog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		carrier := propagation.HeaderCarrier(c.Request.Header)
		ctx := otel.GetTextMapPropagator().Extract(c.Request.Context(), carrier)
		path := c.FullPath()
		if path == "" {
			path = "unmatched"
		}
		spanName := "HTTP " + c.Request.Method + " " + path

		var span trace.Span
		if Tracer() != nil {
			ctx, span = Tracer().Start(ctx, spanName)
			defer span.End()
		}
		c.Request = c.Request.WithContext(ctx)

		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		if span != nil {
			span.SetAttributes(
				attribute.Int("http.status_code", status),
				attribute.String("http.method", c.Request.Method),
				attribute.String("http.route", path),
			)
			if status >= 500 {
				for _, e := range c.Errors {
					span.RecordError(e.Err)
				}
			}
		}
		log := TraceLogger(logger, ctx).With().
			Str("event", "http_request").
			Str("http_method", c.Request.Method).
			Str("http_route", path).
			Int("status", status).
			Int64("duration_ms", time.Since(start).Milliseconds()).
			Logger()
		if status >= 500 {
			log.Error().Msg("request finished")
		} else {
			log.Info().Msg("request finished")
		}
	}
}
