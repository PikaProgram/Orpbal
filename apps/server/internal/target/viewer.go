package target

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"
)

// QueryTimeout bounds every query against a managed target.
const QueryTimeout = 30 * time.Second

// Viewer runs read-only SQL against managed PostgreSQL targets and keeps
// one connection pool per target DSN.
type Viewer struct {
	open  func(dsn string) (*sql.DB, error)
	pools sync.Map // dsn -> *sql.DB
}

// NewViewer builds a Viewer; open defaults to sql.Open with lib/pq.
func NewViewer(open func(dsn string) (*sql.DB, error)) *Viewer {
	if open == nil {
		open = defaultOpen
	}
	return &Viewer{open: open}
}

// pool returns the cached connection for dsn, opening and caching it on first use.
func (v *Viewer) pool(dsn string) (*sql.DB, error) {
	if db, ok := v.pools.Load(dsn); ok {
		return db.(*sql.DB), nil
	}
	db, err := v.open(dsn)
	if err != nil {
		return nil, err
	}
	actual, loaded := v.pools.LoadOrStore(dsn, db)
	if loaded {
		db.Close()
	}
	return actual.(*sql.DB), nil
}

// Query executes one statement in a READ ONLY transaction and returns
// a result set when the statement produces rows.
func (v *Viewer) Query(ctx context.Context, dsn, statement string, limit int) (columns []string, rows [][]any, err error) {
	if err := ReadableStatement(statement); err != nil {
		return nil, nil, err
	}
	db, err := v.pool(dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("open target database: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, nil, fmt.Errorf("begin read-only transaction: %w", err)
	}
	defer tx.Rollback()

	rs, err := tx.QueryContext(ctx, statement)
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
	return columns, rows, tx.Commit()
}

// Execute runs one statement in a writable transaction: commit on success,
// rollback on failure. Used by migrations, not the read-only viewer.
func (v *Viewer) Execute(ctx context.Context, dsn, statement string) error {
	if err := ReadableStatement(statement); err != nil {
		return err
	}
	db, err := v.pool(dsn)
	if err != nil {
		return fmt.Errorf("open target database: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: false})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	if _, err := tx.ExecContext(ctx, statement); err != nil {
		tx.Rollback()
		return fmt.Errorf("execute statement: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
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
