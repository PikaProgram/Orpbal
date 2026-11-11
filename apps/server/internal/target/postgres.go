package target

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"Orpbal/internal/api"
)

// identifier is the strict shape accepted for table and column names in
// hand-built SQL. Values are always parameterized; identifiers cannot be.
var identifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// PostgresDialect implements Dialect for PostgreSQL targets.
type PostgresDialect struct{}

// NewPostgresDialect builds the PostgreSQL dialect.
func NewPostgresDialect() Dialect {
	return &PostgresDialect{}
}

func (d *PostgresDialect) ColumnsSQL() string {
	return `SELECT table_schema, table_name, column_name, data_type, is_nullable,
       ordinal_position, column_default, character_maximum_length
FROM information_schema.columns
WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
ORDER BY table_schema, table_name, ordinal_position`
}

func (d *PostgresDialect) ConstraintsSQL() string {
	return `SELECT tc.table_schema, tc.table_name, tc.constraint_type, tc.constraint_name,
       kcu.column_name,
       ccu.table_name AS referenced_table,
       ccu.column_name AS referenced_column
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
  ON tc.constraint_name = kcu.constraint_name
 AND tc.constraint_schema = kcu.table_schema
LEFT JOIN information_schema.constraint_column_usage ccu
  ON tc.constraint_name = ccu.constraint_name
 AND tc.constraint_schema = ccu.table_schema
WHERE tc.table_schema NOT IN ('pg_catalog', 'information_schema')
ORDER BY tc.table_schema, tc.table_name, tc.constraint_name, kcu.ordinal_position`
}

func (d *PostgresDialect) IndexesSQL() string {
	return `SELECT schemaname, tablename, indexname, indexdef
FROM pg_indexes
WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
ORDER BY schemaname, tablename, indexname`
}

func (d *PostgresDialect) CountRowsSQL() string {
	return `SELECT n.nspname AS table_schema,
       c.relname AS table_name,
       (SELECT count(*) FROM information_schema.columns col
         WHERE col.table_schema = n.nspname AND col.table_name = c.relname) AS column_count,
       CASE WHEN c.reltuples >= 0 THEN c.reltuples ELSE NULL END AS estimate
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind = 'r'
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
ORDER BY n.nspname, c.relname`
}

// QuoteIdent validates and double-quotes an identifier for interpolation.
func (d *PostgresDialect) QuoteIdent(value string) (string, error) {
	if !identifier.MatchString(value) {
		return "", fmt.Errorf("invalid identifier %q", value)
	}
	return `"` + value + `"`, nil
}

// Mutation renders the parameterized SQL for a row mutation. The table and
// column names come from information_schema listings, but they are still
// validated against the identifier pattern before quoting.
func (d *PostgresDialect) Mutation(body api.MutateRowsRequest) (string, []any, error) {
	schemaName, err := d.QuoteIdent(body.Schema)
	if err != nil {
		return "", nil, fmt.Errorf("invalid identifier %q: %w", body.Schema, ErrInvalidMutation)
	}
	tableName, err := d.QuoteIdent(body.Table)
	if err != nil {
		return "", nil, fmt.Errorf("invalid identifier %q: %w", body.Table, ErrInvalidMutation)
	}
	qualified := schemaName + "." + tableName

	switch body.Op {
	case api.Insert:
		if body.Values == nil || len(*body.Values) == 0 {
			return "", nil, fmt.Errorf("insert requires values: %w", ErrInvalidMutation)
		}
		values := *body.Values
		columns := make([]string, 0, len(values))
		placeholders := make([]string, 0, len(values))
		args := make([]any, 0, len(values))
		i := 1
		for _, name := range sortedKeys(values) {
			column, err := d.QuoteIdent(name)
			if err != nil {
				return "", nil, fmt.Errorf("invalid identifier %q: %w", name, ErrInvalidMutation)
			}
			columns = append(columns, column)
			placeholders = append(placeholders, fmt.Sprintf("$%d", i))
			args = append(args, values[name])
			i++
		}
		return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", qualified,
			strings.Join(columns, ", "), strings.Join(placeholders, ", ")), args, nil

	case api.Update:
		if body.Pk == nil || len(*body.Pk) == 0 {
			return "", nil, fmt.Errorf("update requires the primary key values: %w", ErrInvalidMutation)
		}
		if body.Values == nil || len(*body.Values) == 0 {
			return "", nil, fmt.Errorf("update requires at least one changed column: %w", ErrInvalidMutation)
		}
		values, pk := *body.Values, *body.Pk
		sets := make([]string, 0, len(values))
		args := make([]any, 0, len(values)+len(pk))
		i := 1
		for _, name := range sortedKeys(values) {
			column, err := d.QuoteIdent(name)
			if err != nil {
				return "", nil, fmt.Errorf("invalid identifier %q: %w", name, ErrInvalidMutation)
			}
			sets = append(sets, fmt.Sprintf("%s = $%d", column, i))
			args = append(args, values[name])
			i++
		}
		where, err := pkWhere(pk, &args, &i, d.QuoteIdent)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("UPDATE %s SET %s WHERE %s", qualified, strings.Join(sets, ", "), where), args, nil

	case api.Delete:
		if body.Pk == nil || len(*body.Pk) == 0 {
			return "", nil, fmt.Errorf("delete requires the primary key values: %w", ErrInvalidMutation)
		}
		pk := *body.Pk
		args := make([]any, 0, len(pk))
		i := 1
		where, err := pkWhere(pk, &args, &i, d.QuoteIdent)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("DELETE FROM %s WHERE %s", qualified, where), args, nil

	default:
		return "", nil, fmt.Errorf("unknown op %q: %w", string(body.Op), ErrInvalidMutation)
	}
}

func (d *PostgresDialect) BeginReadOnly(ctx context.Context, db *sql.DB) (*sql.Tx, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin read-only transaction: %w", err)
	}
	return tx, nil
}

func (d *PostgresDialect) GuardRead(statement string) error {
	return ReadableStatement(statement)
}

func (d *PostgresDialect) Explain(ctx context.Context, db *sql.DB, statement string, analyze bool) (ExplainResult, error) {
	options := "FORMAT JSON"
	if analyze {
		options += ", ANALYZE, BUFFERS, TIMING true"
	}
	query := "EXPLAIN (" + options + ") " + statement

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ExplainResult{}, fmt.Errorf("begin read-only transaction: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return ExplainResult{}, fmt.Errorf("explain query: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return ExplainResult{}, fmt.Errorf("read explain result: %w", err)
		}
		return ExplainResult{}, fmt.Errorf("explain returned no plan")
	}
	var value any
	if err := rows.Scan(&value); err != nil {
		return ExplainResult{}, fmt.Errorf("scan explain result: %w", err)
	}
	if err := rows.Err(); err != nil {
		return ExplainResult{}, fmt.Errorf("iterate explain result: %w", err)
	}

	nodes, err := parsePostgresExplain(toBytes(value))
	if err != nil {
		return ExplainResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return ExplainResult{}, fmt.Errorf("commit explain transaction: %w", err)
	}
	return ExplainResult{Engine: "postgres", Analyze: analyze, Nodes: nodes}, nil
}

// pkWhere renders "col = $n AND ..." for the primary key columns.
func pkWhere(pk map[string]any, args *[]any, i *int, quote func(string) (string, error)) (string, error) {
	parts := make([]string, 0, len(pk))
	for _, name := range sortedKeys(pk) {
		column, err := quote(name)
		if err != nil {
			return "", fmt.Errorf("invalid identifier %q: %w", name, ErrInvalidMutation)
		}
		parts = append(parts, fmt.Sprintf("%s = $%d", column, *i))
		*args = append(*args, pk[name])
		*i++
	}
	return strings.Join(parts, " AND "), nil
}

// sortedKeys fixes map iteration order so SQL and placeholders are deterministic.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
