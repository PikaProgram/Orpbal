package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

var (
	sensitiveKey   = regexp.MustCompile(`(?i)(password|passwd|secret|token|cookie|authorization|credential|session|dsn)`)
	sensitiveValue = regexp.MustCompile(`(?i)(password|passwd|pwd|token|secret|apikey|api_key|key)=([^&\s,;"']+)`)
)

// LogWriter keeps the existing stdout/stderr log stream and mirrors each JSON
// line to the OTel Collector as an OTLP log record. When no OTLP endpoint is
// configured it is a transparent pass-through.
type LogWriter struct {
	base   io.Writer
	logger otellog.Logger
}

// NewLogWriter wraps base. Set OTEL_EXPORTER_OTLP_ENDPOINT (or the logs-
// specific variant) to enable OTLP log export; otherwise logs stay local.
func NewLogWriter(base io.Writer, serviceName string) (*LogWriter, func(context.Context) error, error) {
	if base == nil {
		base = os.Stdout
	}
	w := &LogWriter{base: base}
	noop := func(context.Context) error { return nil }
	if !hasOTLPEndpoint("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT") {
		return w, noop, nil
	}

	ctx := context.Background()
	exp, err := otlploghttp.New(ctx)
	if err != nil {
		return w, noop, fmt.Errorf("otlp log exporter: %w", err)
	}
	res, err := resource.Merge(resource.Default(),
		resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(serviceName)))
	if err != nil {
		res = resource.Default()
	}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewBatchProcessor(exp)), sdklog.WithResource(res))
	w.logger = provider.Logger(serviceName)
	return w, provider.Shutdown, nil
}

func hasOTLPEndpoint(specific string) bool {
	return os.Getenv(specific) != "" || os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != ""
}

// JSONLogs reports whether zerolog should emit JSON (required for the log
// bridge to extract trace_id/span_id and the structured attributes).
func JSONLogs() bool {
	return os.Getenv("LOG_LEVEL") == "production" || hasOTLPEndpoint("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT")
}

func (w *LogWriter) Write(p []byte) (int, error) {
	out := p
	if safe := scrubText(string(p)); safe != string(p) {
		out = []byte(safe)
	}
	n, err := w.base.Write(out)
	if err == nil && w.logger != nil {
		w.emit(string(out))
	}
	if len(out) != len(p) {
		// Redaction shortened the line: the original slice was consumed.
		return len(p), err
	}
	return n, err
}

func scrubText(line string) string {
	return sensitiveValue.ReplaceAllString(line, "${1}=[REDACTED]")
}

func (w *LogWriter) emit(line string) {
	var fields map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &fields); err != nil {
		// Console format (dev): still ship the text, without structured fields.
		fields = map[string]any{"message": strings.TrimSpace(line)}
	}

	ctx := context.Background()
	if sc, ok := spanContextOf(fields); ok {
		ctx = trace.ContextWithSpanContext(ctx, sc)
	}

	now := time.Now()
	var record otellog.Record
	record.SetTimestamp(now)
	record.SetObservedTimestamp(now)
	level := fmt.Sprint(fields["level"])
	record.SetSeverityText(level)
	record.SetSeverity(severityOf(level))
	if message, ok := fields["message"].(string); ok {
		record.SetBody(attribute.StringValue(message))
	} else {
		record.SetBody(attribute.StringValue(strings.TrimSpace(line)))
	}
	if event, ok := fields["event"].(string); ok && event != "" {
		record.SetEventName(event)
	}
	for key, value := range fields {
		if sensitiveKey.MatchString(key) || key == "level" || key == "time" || key == "message" {
			continue
		}
		record.AddAttributes(logAttribute(key, value))
	}
	w.logger.Emit(ctx, record)
}

func spanContextOf(fields map[string]any) (trace.SpanContext, bool) {
	traceID, err := trace.TraceIDFromHex(fmt.Sprint(fields["trace_id"]))
	if err != nil {
		return trace.SpanContext{}, false
	}
	spanID, err := trace.SpanIDFromHex(fmt.Sprint(fields["span_id"]))
	if err != nil {
		return trace.SpanContext{}, false
	}
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	}), true
}

func severityOf(level string) otellog.Severity {
	switch strings.ToLower(level) {
	case "trace":
		return otellog.SeverityTrace
	case "debug":
		return otellog.SeverityDebug
	case "warn", "warning":
		return otellog.SeverityWarn
	case "error":
		return otellog.SeverityError
	case "fatal", "panic":
		return otellog.SeverityFatal
	default:
		return otellog.SeverityInfo
	}
}

func logAttribute(key string, value any) attribute.KeyValue {
	switch v := value.(type) {
	case bool:
		return attribute.Bool(key, v)
	case float64:
		return attribute.Float64(key, v)
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return attribute.Int64(key, n)
		}
		return attribute.String(key, v.String())
	case string:
		if len(v) > 2048 {
			v = v[:2048] + "...[truncated]"
		}
		return attribute.String(key, v)
	default:
		text := fmt.Sprint(v)
		if len(text) > 2048 {
			text = text[:2048] + "...[truncated]"
		}
		return attribute.String(key, text)
	}
}
