package target

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"Orpbal/internal/api"
)

// MySQLDialect implements Dialect for MySQL targets.
type MySQLDialect struct{}

// NewMySQLDialect builds the MySQL dialect.
func NewMySQLDialect() Dialect {
	return &MySQLDialect{}
}

func (d *MySQLDialect) ColumnsSQL() string {
	return `SELECT DATABASE() AS table_schema, table_name, column_name, data_type, is_nullable,
       ordinal_position, column_default, character_maximum_length
FROM information_schema.columns
WHERE table_schema = DATABASE()
ORDER BY table_name, ordinal_position`
}

func (d *MySQLDialect) ConstraintsSQL() string {
	return `SELECT tc.table_schema, tc.table_name, tc.constraint_type, tc.constraint_name,
       kcu.column_name,
       kcu.referenced_table_name AS referenced_table,
       kcu.referenced_column_name AS referenced_column
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
  ON tc.constraint_name = kcu.constraint_name
 AND tc.constraint_schema = kcu.table_schema
 AND tc.table_name = kcu.table_name
WHERE tc.table_schema = DATABASE()
ORDER BY tc.table_name, tc.constraint_name, kcu.ordinal_position`
}

func (d *MySQLDialect) IndexesSQL() string {
	return `SELECT table_schema, table_name, index_name,
       CONCAT('CREATE ', IF(MAX(non_unique)=0, 'UNIQUE ', ''), 'INDEX ', index_name,
              ' ON ', table_name, ' (', GROUP_CONCAT(column_name ORDER BY seq_in_index SEPARATOR ', '), ')') AS indexdef
FROM information_schema.statistics
WHERE table_schema = DATABASE() AND index_name <> 'PRIMARY'
GROUP BY table_schema, table_name, index_name
ORDER BY table_schema, table_name, index_name`
}

// CountRowsSQL lists every table with its column count and the
// information_schema estimated row count; the estimate is nullable.
func (d *MySQLDialect) CountRowsSQL() string {
	return `SELECT t.table_schema,
       t.table_name,
       (SELECT count(*) FROM information_schema.columns c
         WHERE c.table_schema = t.table_schema AND c.table_name = t.table_name) AS column_count,
       t.table_rows AS estimate
FROM information_schema.tables t
WHERE t.table_schema = DATABASE() AND t.table_type = 'BASE TABLE'
ORDER BY t.table_schema, t.table_name`
}

// QuoteIdent validates and backtick-quotes an identifier for interpolation.
func (d *MySQLDialect) QuoteIdent(value string) (string, error) {
	if !identifier.MatchString(value) {
		return "", fmt.Errorf("invalid identifier %q", value)
	}
	return "`" + value + "`", nil
}

// Mutation renders the parameterized SQL for a row mutation. The table and
// column names come from information_schema listings, but they are still
// validated against the identifier pattern before quoting.
func (d *MySQLDialect) Mutation(body api.MutateRowsRequest) (string, []any, error) {
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
		for _, name := range sortedKeys(values) {
			column, err := d.QuoteIdent(name)
			if err != nil {
				return "", nil, fmt.Errorf("invalid identifier %q: %w", name, ErrInvalidMutation)
			}
			columns = append(columns, column)
			placeholders = append(placeholders, "?")
			args = append(args, values[name])
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
		for _, name := range sortedKeys(values) {
			column, err := d.QuoteIdent(name)
			if err != nil {
				return "", nil, fmt.Errorf("invalid identifier %q: %w", name, ErrInvalidMutation)
			}
			sets = append(sets, column+" = ?")
			args = append(args, values[name])
		}
		where, err := mysqlPkWhere(pk, &args, d.QuoteIdent)
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
		where, err := mysqlPkWhere(pk, &args, d.QuoteIdent)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("DELETE FROM %s WHERE %s", qualified, where), args, nil

	default:
		return "", nil, fmt.Errorf("unknown op %q: %w", string(body.Op), ErrInvalidMutation)
	}
}

// mysqlPkWhere renders "col = ? AND ..." for the primary key columns,
// appending the primary key values to args in sorted-key order.
func mysqlPkWhere(pk map[string]any, args *[]any, quote func(string) (string, error)) (string, error) {
	parts := make([]string, 0, len(pk))
	for _, name := range sortedKeys(pk) {
		column, err := quote(name)
		if err != nil {
			return "", fmt.Errorf("invalid identifier %q: %w", name, ErrInvalidMutation)
		}
		parts = append(parts, column+" = ?")
		*args = append(*args, pk[name])
	}
	return strings.Join(parts, " AND "), nil
}

// BeginReadOnly returns a nil transaction: MySQL has no read-only
// transaction, so the GuardRead allowlist plus the driver's
// MultiStatements=false guard read-only execution instead.
func (d *MySQLDialect) BeginReadOnly(ctx context.Context, db *sql.DB) (*sql.Tx, error) {
	return nil, nil
}

func (d *MySQLDialect) GuardRead(statement string) error {
	return MySQLReadableStatement(statement)
}

// Explain uses the engine's textual EXPLAIN output. MySQL has no
// read-only transaction here, so the same allowlist as the viewer applies.
func (d *MySQLDialect) Explain(ctx context.Context, db *sql.DB, statement string, analyze bool) (ExplainResult, error) {
	if err := MySQLReadableStatement(statement); err != nil {
		return ExplainResult{}, err
	}
	prefix := "EXPLAIN "
	if analyze {
		prefix = "EXPLAIN ANALYZE "
	}
	rows, err := db.QueryContext(ctx, prefix+statement)
	if err != nil {
		return ExplainResult{}, fmt.Errorf("explain query: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return ExplainResult{}, fmt.Errorf("read explain columns: %w", err)
	}
	result := [][]any{}
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return ExplainResult{}, fmt.Errorf("scan explain row: %w", err)
		}
		for i := range values {
			values[i] = FormatValue(values[i])
		}
		result = append(result, values)
	}
	if err := rows.Err(); err != nil {
		return ExplainResult{}, fmt.Errorf("iterate explain rows: %w", err)
	}
	return ExplainResult{
		Engine:  "mysql",
		Analyze: analyze,
		Nodes:   []ExplainNode{},
		Columns: columns,
		Rows:    result,
	}, nil
}
