package target

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"Orpbal/internal/api"
	"Orpbal/internal/schema/build"
)

// QueryTimeout bounds every query against a managed target.
const QueryTimeout = 30 * time.Second

// Dialect adapts a stored target to one engine (postgres or mysql): the
// introspection and count SQL, identifier quoting, placeholder style, the
// read-only guard strategy, and the mutation builder.
type Dialect interface {
	ColumnsSQL() string
	ConstraintsSQL() string
	IndexesSQL() string
	CountRowsSQL() string
	QuoteIdent(value string) (string, error)
	Mutation(body api.MutateRowsRequest) (string, []any, error)
	BeginReadOnly(ctx context.Context, db *sql.DB) (*sql.Tx, error)
	GuardRead(statement string) error
	Explain(ctx context.Context, db *sql.DB, statement string, analyze bool) (ExplainResult, error)
}

// Target is one managed database connection: its pool handle plus the engine dialect.
type Target struct {
	db      *sql.DB
	dialect Dialect
}

// ErrInvalidMutation wraps dialect mutation validation errors so handlers can
// map them to 400.
var ErrInvalidMutation = errors.New("invalid mutation request")

// Query executes one statement using the dialect's read-only guard and
// transaction strategy, returning a result set when the statement produces
// rows.
func (t *Target) Query(ctx context.Context, statement string, limit int) (columns []string, rows [][]any, err error) {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()

	if err := t.dialect.GuardRead(statement); err != nil {
		return nil, nil, err
	}

	tx, err := t.dialect.BeginReadOnly(ctx, t.db)
	if err != nil {
		return nil, nil, fmt.Errorf("begin read-only transaction: %w", err)
	}

	var rs *sql.Rows
	if tx != nil {
		defer tx.Rollback()
		rs, err = tx.QueryContext(ctx, statement)
	} else {
		rs, err = t.db.QueryContext(ctx, statement)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("execute query: %w", err)
	}
	defer rs.Close()

	columns, err = rs.Columns()
	if err != nil {
		return nil, nil, fmt.Errorf("read columns: %w", err)
	}

	for rs.Next() && (limit <= 0 || len(rows) < limit) {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rs.Scan(pointers...); err != nil {
			return nil, nil, fmt.Errorf("scan row: %w", err)
		}
		rows = append(rows, values)
	}
	if err := rs.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate rows: %w", err)
	}
	if tx != nil {
		return columns, rows, tx.Commit()
	}
	return columns, rows, nil
}

// Execute runs one statement in a writable transaction: commit on success,
// rollback on failure.
func (t *Target) Execute(ctx context.Context, statement string) error {
	_, err := t.ExecuteArgs(ctx, statement, nil)
	return err
}

// ExecuteArgs runs one parameterized statement in a writable transaction and
// returns the number of affected rows. Used by row mutations and migrations.
func (t *Target) ExecuteArgs(ctx context.Context, statement string, args []any) (int64, error) {
	if err := ReadableStatement(statement); err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()

	tx, err := t.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: false})
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	result, err := tx.ExecContext(ctx, statement, args...)
	if err != nil {
		tx.Rollback()
		return 0, fmt.Errorf("execute statement: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		tx.Rollback()
		return 0, fmt.Errorf("read affected rows: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit transaction: %w", err)
	}
	return affected, nil
}

// Explain runs EXPLAIN (optionally ANALYZE) for one statement via the dialect.
func (t *Target) Explain(ctx context.Context, statement string, analyze bool) (ExplainResult, error) {
	if err := ReadableStatement(statement); err != nil {
		return ExplainResult{}, err
	}
	if firstKeyword(statement) == "EXPLAIN" {
		return ExplainResult{}, fmt.Errorf("remove the leading EXPLAIN; the planner adds it")
	}
	return t.dialect.Explain(ctx, t.db, statement, analyze)
}

// IntrospectSchema reports the database schema (tables, columns, constraints,
// indexes) for the target's engine.
func (t *Target) IntrospectSchema(ctx context.Context) (api.DatabaseSchema, error) {
	cols, colRows, err := t.Query(ctx, t.dialect.ColumnsSQL(), 0)
	if err != nil {
		return api.DatabaseSchema{}, err
	}
	_, constraintRows, err := t.Query(ctx, t.dialect.ConstraintsSQL(), 0)
	if err != nil {
		return api.DatabaseSchema{}, err
	}
	_, indexRows, err := t.Query(ctx, t.dialect.IndexesSQL(), 0)
	if err != nil {
		return api.DatabaseSchema{}, err
	}
	return build.BuildSchema(cols, colRows, constraintRows, indexRows), nil
}

// TableCounts lists every table with its column count and estimated row count.
func (t *Target) TableCounts(ctx context.Context) ([]api.TableCount, error) {
	_, rows, err := t.Query(ctx, t.dialect.CountRowsSQL(), 0)
	if err != nil {
		return nil, err
	}
	counts := make([]api.TableCount, 0, len(rows))
	for _, row := range rows {
		var estimate *int
		if f, ok := row[3].(float64); ok && f >= 0 {
			n := int(f)
			estimate = &n
		}
		counts = append(counts, api.TableCount{
			Schema:   toString(row[0]),
			Table:    toString(row[1]),
			Columns:  int(toInt64(row[2])),
			Estimate: estimate,
		})
	}
	return counts, nil
}

// Mutate applies one insert, update, or delete rendered by the dialect.
func (t *Target) Mutate(ctx context.Context, body api.MutateRowsRequest) (int64, error) {
	stmt, args, err := t.dialect.Mutation(body)
	if err != nil {
		return 0, err
	}
	return t.ExecuteArgs(ctx, stmt, args)
}

// FormatValue renders a SQL value for the JSON API; nil stays nil (SQL NULL).
func FormatValue(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(t)
	case time.Time:
		return t.Format(time.RFC3339)
	default:
		return v
	}
}

// TruncateCell caps rendered cell text to keep payloads and pages sane.
func TruncateCell(v any, max int) any {
	if s, ok := v.(string); ok && len(s) > max {
		return s[:max] + "…"
	}
	return v
}

// ReadableStatement rejects empty input; the read-only transaction already
// blocks writes, this just fails fast on obvious misuse.
func ReadableStatement(s string) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("sql statement is required")
	}
	return nil
}

// mysqlReadableKeywords allowlists the statement types a read-only MySQL
// query may start with. MultiStatements=false blocks `;` concatenation, so
// the first keyword decides.
var mysqlReadableKeywords = map[string]bool{
	"SELECT":   true,
	"SHOW":     true,
	"EXPLAIN":  true,
	"DESCRIBE": true,
	"DESC":     true,
	"WITH":     true, // CTEs in MySQL 8 can only precede SELECT
}

// MySQLReadableStatement enforces read-only semantics for MySQL targets:
// only statements that cannot mutate state pass.
func MySQLReadableStatement(s string) error {
	if err := ReadableStatement(s); err != nil {
		return err
	}
	if first := strings.ToUpper(strings.Fields(s)[0]); !mysqlReadableKeywords[first] {
		return fmt.Errorf("only SELECT, SHOW, EXPLAIN, DESCRIBE, or WITH statements are allowed")
	}
	return nil
}

// toString renders a SQL value as a string; non-string scalars fall back to "".
func toString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(t)
	case string:
		return t
	default:
		return ""
	}
}

// toInt64 widens the numeric shapes lib/pq may hand back.
func toInt64(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int32:
		return int64(t)
	case float64:
		return int64(t)
	case []byte:
		var n int64
		fmt.Sscanf(string(t), "%d", &n)
		return n
	default:
		return 0
	}
}
