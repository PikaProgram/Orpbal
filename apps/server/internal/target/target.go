package target

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"Orpbal/internal/api"
	"Orpbal/internal/schema/build"
	"Orpbal/internal/sqllog"
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
// Name/ID/Engine are filled by the Manager so every SQL audit log line can
// carry the logical database name (not just the numeric id) per the M2
// logging attributes.
type Target struct {
	db      *sql.DB
	dialect Dialect
	ID      int
	Name    string
	Engine  string
}

// Dialect exposes the engine adapter (identifier quoting for worker SQL).
func (t *Target) Dialect() Dialect { return t.dialect }

// Ping verifies pool reachability with a trivial round trip (admin
// connection test endpoint).
func (t *Target) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return t.db.PingContext(ctx)
}

// ErrInvalidMutation wraps dialect mutation validation errors so handlers can
// map them to 400.
var ErrInvalidMutation = errors.New("invalid mutation request")

// audit returns the ctx-attached SQL audit logger enriched with this target's
// identity. ok is false when the caller attached no logger (unit/smoke tests)
// and the call site can skip logging.
func (t *Target) audit(ctx context.Context) (zerolog.Logger, bool) {
	base, ok := sqllog.From(ctx)
	if !ok {
		return zerolog.Nop(), false
	}
	return base.With().
		Str("database", t.Name).
		Int("database_id", t.ID).
		Str("engine", t.Engine).
		Logger(), true
}

// Query executes one statement using the dialect's read-only guard and
// transaction strategy, returning a result set when the statement produces
// rows.
func (t *Target) Query(ctx context.Context, statement string, limit int) (columns []string, rows [][]any, err error) {
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()

	if err := t.dialect.GuardRead(statement); err != nil {
		return nil, nil, err
	}

	if log, ok := t.audit(ctx); ok {
		log.Info().Str("event", "sql_query_start").Str("status", "running").
			Str("sql", sqllog.LogSQL(statement)).Msg("read-only query started")
	}
	start := time.Now()

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
		t.logReadFailure(ctx, statement, err)
		return nil, nil, fmt.Errorf("execute query: %w", err)
	}
	defer rs.Close()

	columns, err = rs.Columns()
	if err != nil {
		t.logReadFailure(ctx, statement, err)
		return nil, nil, fmt.Errorf("read columns: %w", err)
	}

	for rs.Next() && (limit <= 0 || len(rows) < limit) {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rs.Scan(pointers...); err != nil {
			t.logReadFailure(ctx, statement, err)
			return nil, nil, fmt.Errorf("scan row: %w", err)
		}
		rows = append(rows, values)
	}
	if err := rs.Err(); err != nil {
		t.logReadFailure(ctx, statement, err)
		return nil, nil, fmt.Errorf("iterate rows: %w", err)
	}
	if tx != nil {
		err := tx.Commit()
		if log, ok := t.audit(ctx); ok {
			duration := time.Since(start).Milliseconds()
			if err != nil {
				log.Error().Str("event", "sql_query_done").Str("status", "failed").
					Str("sql", sqllog.LogSQL(statement)).
					Int("rows", len(rows)).
					Int64("duration_ms", duration).
					Str("error", sqllog.ErrorText(err)).
					Str("sqlstate", sqllog.SQLState(err)).
					Str("phase", "commit").
					Msg("read-only query failed to commit")
			} else {
				log.Info().Str("event", "sql_query_done").Str("status", "success").
					Str("sql", sqllog.LogSQL(statement)).
					Int("rows", len(rows)).Int("row_limit", limit).
					Int64("duration_ms", duration).
					Str("phase", "commit").
					Msg("read-only query finished")
			}
		}
		return columns, rows, err
	}
	return columns, rows, nil
}

// logReadFailure emits one failed read line (start/result are separate per
// the spec; failures carry SQLSTATE when the driver exposes it).
func (t *Target) logReadFailure(ctx context.Context, statement string, err error) {
	if log, ok := t.audit(ctx); ok {
		log.Error().Str("event", "sql_query_done").Str("status", "failed").
			Str("sql", sqllog.LogSQL(statement)).
			Str("error", sqllog.ErrorText(err)).
			Str("sqlstate", sqllog.SQLState(err)).
			Msg("read-only query failed")
	}
}

// Execute runs one statement in a writable transaction: commit on success,
// rollback on failure.
func (t *Target) Execute(ctx context.Context, statement string) error {
	_, err := t.ExecuteArgs(ctx, statement, nil)
	return err
}

// ExecuteArgs runs one parameterized statement in a writable transaction and
// returns the number of affected rows. Used by row mutations and migrations.
// The write path logs start, commit, and rollback as separate events so the
// audit trail distinguishes the result from the transaction end state.
func (t *Target) ExecuteArgs(ctx context.Context, statement string, args []any) (int64, error) {
	if err := ReadableStatement(statement); err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()

	if log, ok := t.audit(ctx); ok {
		log.Info().Str("event", "sql_exec_start").Str("status", "running").
			Str("sql", sqllog.LogSQL(statement)).
			Int("param_count", len(args)).
			Msg("statement started")
	}
	start := time.Now()

	tx, err := t.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: false})
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	result, err := tx.ExecContext(ctx, statement, args...)
	if err != nil {
		if rErr := tx.Rollback(); rErr != nil {
			if log, ok := t.audit(ctx); ok {
				log.Error().Str("event", "sql_rollback").Str("status", "failed").
					Str("error", sqllog.ErrorText(rErr)).
					Str("sqlstate", sqllog.SQLState(rErr)).
					Msg("transaction rollback failed")
			}
		}
		if log, ok := t.audit(ctx); ok {
			log.Error().Str("event", "sql_rollback").Str("status", "rolled_back").
				Int64("duration_ms", time.Since(start).Milliseconds()).
				Str("sql", sqllog.LogSQL(statement)).
				Str("error", sqllog.ErrorText(err)).
				Str("sqlstate", sqllog.SQLState(err)).
				Msg("statement failed; transaction rolled back")
		}
		return 0, fmt.Errorf("execute statement: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		if rErr := tx.Rollback(); rErr != nil {
			if log, ok := t.audit(ctx); ok {
				log.Error().Str("event", "sql_rollback").Str("status", "failed").
					Str("error", sqllog.ErrorText(rErr)).
					Str("sqlstate", sqllog.SQLState(rErr)).
					Msg("transaction rollback failed")
			}
		}
		if log, ok := t.audit(ctx); ok {
			log.Error().Str("event", "sql_rollback").Str("status", "rolled_back").
				Int64("duration_ms", time.Since(start).Milliseconds()).
				Str("sql", sqllog.LogSQL(statement)).
				Str("error", sqllog.ErrorText(err)).
				Str("sqlstate", sqllog.SQLState(err)).
				Msg("affected-row read failed; transaction rolled back")
		}
		return 0, fmt.Errorf("read affected rows: %w", err)
	}
	if err := tx.Commit(); err != nil {
		if log, ok := t.audit(ctx); ok {
			log.Error().Str("event", "sql_commit").Str("status", "failed").
				Int64("duration_ms", time.Since(start).Milliseconds()).
				Str("error", sqllog.ErrorText(err)).
				Str("sqlstate", sqllog.SQLState(err)).
				Msg("transaction commit failed")
		}
		return 0, fmt.Errorf("commit transaction: %w", err)
	}
	if log, ok := t.audit(ctx); ok {
		log.Info().Str("event", "sql_commit").Str("status", "committed").
			Int64("rows", affected).
			Int64("duration_ms", time.Since(start).Milliseconds()).
			Str("sql", sqllog.LogSQL(statement)).
			Msg("statement committed")
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
	if log, ok := t.audit(ctx); ok {
		log.Info().Str("event", "sql_explain").Str("status", "running").
			Str("sql", sqllog.LogSQL(statement)).
			Bool("analyze", analyze).
			Msg("execution plan requested")
	}
	start := time.Now()
	result, err := t.dialect.Explain(ctx, t.db, statement, analyze)
	if log, ok := t.audit(ctx); ok {
		line := log.Info().Str("event", "sql_explain_done").
			Int64("duration_ms", time.Since(start).Milliseconds()).
			Str("sql", sqllog.LogSQL(statement))
		if err != nil {
			line = log.Error().Str("event", "sql_explain_done").Str("status", "failed").
				Str("error", sqllog.ErrorText(err)).
				Str("sqlstate", sqllog.SQLState(err))
		} else {
			line = line.Str("status", "success").Int("plan_nodes", len(result.Nodes))
		}
		line.Msg("execution plan finished")
	}
	return result, err
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
