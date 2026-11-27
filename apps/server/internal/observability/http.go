package observability

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"Orpbal/internal/tracing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// HTTPMiddleware starts one span per API request, extracting an incoming W3C
// traceparent so a caller can join the trace. The request context carrying the
// span reaches the migration service, which stores the traceparent on the job
// row for the worker to resume.
func HTTPMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, span := tracing.StartSpan(c.Request.Context(), c.GetHeader("traceparent"), "http "+c.Request.Method+" "+c.FullPath())
		defer span.End()
		c.Request = c.Request.WithContext(ctx)

		c.Next()

		status := c.Writer.Status()
		span.SetAttributes(attribute.Int("http.response.status_code", status))
		if status >= 500 {
			span.SetStatus(codes.Error, httpStatusText(status))
		}
	}
}

func httpStatusText(status int) string {
	return fmt.Sprintf("HTTP %d", status)
}
