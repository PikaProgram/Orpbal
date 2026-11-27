package target

import (
	"context"
	"strings"

	"Orpbal/internal/tracing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// statementSpan adds a db.* span to the trace resumed by the worker (or the
// HTTP span in the API). Only the operation verb is recorded: statement text
// and parameters stay in the structured logs, never as span/metric labels.
func statementSpan(ctx context.Context, dialect Dialect, statement, operation string) (context.Context, trace.Span) {
	if operation == "" {
		operation = firstKeyword(statement)
	}
	ctx, span := tracing.StartSpan(ctx, "", "db."+strings.ToLower(operation))
	system := "postgresql"
	if _, ok := dialect.(*MySQLDialect); ok {
		system = "mysql"
	}
	span.SetAttributes(
		attribute.String("db.system", system),
		attribute.String("db.operation", operation),
	)
	return ctx, span
}

func finishStatementSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}
