package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"Orpbal/ent"
	"Orpbal/ent/databaseconnection"
	"Orpbal/internal/access"
	"Orpbal/internal/api"
	"Orpbal/internal/security/dsn"
	"Orpbal/internal/target"
)

// GetDatabaseSchema lists every table with columns, constraints, and indexes
// in a managed target. Requires effective access (view_only or view_edit).
func (h *Handler) GetDatabaseSchema(c *gin.Context, id api.Id) {
	user := h.currentUser(c)
	if user == nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	level, ok := h.requireAccess(c, user, id)
	if !ok {
		return
	}
	_ = level // schema and data viewing are allowed for any effective access
	connection, dsnStr, ok := h.loadTargetDSN(c, id)
	if !ok {
		return
	}
	var colsSQL, consSQL, idxsSQL string
	if connection.Engine == databaseconnection.EngineMysql {
		colsSQL, consSQL, idxsSQL = mysqlColumnsSQL, mysqlConstraintsSQL, mysqlIndexesSQL
	} else {
		colsSQL, consSQL, idxsSQL = columnsSQL, constraintsSQL, indexesSQL
	}
	colCols, colRows, err := h.viewer.Query(c.Request.Context(), dsnStr, colsSQL, 0)
	if err != nil {
		writeError(c, http.StatusBadGateway, "target_unreachable", err.Error())
		return
	}
	_, constraintRows, err := h.viewer.Query(c.Request.Context(), dsnStr, consSQL, 0)
	if err != nil {
		writeError(c, http.StatusBadGateway, "target_unreachable", err.Error())
		return
	}
	_, indexRows, err := h.viewer.Query(c.Request.Context(), dsnStr, idxsSQL, 0)
	if err != nil {
		writeError(c, http.StatusBadGateway, "target_unreachable", err.Error())
		return
	}
	schema := buildSchema(colCols, colRows, constraintRows, indexRows)
	c.JSON(http.StatusOK, schema)
}

// ExecuteDatabaseQuery runs one read-only statement against a managed target.
func (h *Handler) ExecuteDatabaseQuery(c *gin.Context, id api.Id, params api.ExecuteDatabaseQueryParams) {
	body := api.ExecuteQueryRequest{}
	if !bindJSON(c, &body) {
		return
	}
	if err := target.ReadableStatement(body.Sql); err != nil {
		writeFields(c, map[string]string{"sql": err.Error()})
		return
	}
	user := h.currentUser(c)
	if user == nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if _, ok := h.requireAccess(c, user, id); !ok {
		return
	}
	_, dsnStr, ok := h.loadTargetDSN(c, id)
	if !ok {
		return
	}
	limit := 100
	if body.Limit != nil {
		limit = *body.Limit
	}
	// Zero/negative would loop uncapped in the scan, bypassing the ceiling.
	if limit < 1 {
		limit = 1
	}
	// Hard server ceiling: an uncapped SELECT against a big target would
	// serialize the entire result into one JSON response.
	const hardCeiling = 10000
	if limit > hardCeiling {
		limit = hardCeiling
	}
	// Offset is applied in Go while scanning, so any SQL statement paginates
	// without the handler having to wrap user SQL.
	offset := 0
	if body.Offset != nil && *body.Offset > 0 {
		offset = *body.Offset
	}
	columns, rows, err := h.viewer.Query(c.Request.Context(), dsnStr, body.Sql, limit+offset)
	if err == nil && offset > 0 {
		if offset >= len(rows) {
			rows = nil
		} else {
			rows = rows[offset:]
		}
	}
	if err != nil {
		writeError(c, http.StatusBadGateway, "target_unreachable", err.Error())
		return
	}
	result := api.QueryResult{Columns: columns, Rows: make([]*[]any, 0, len(rows))}
	for _, row := range rows {
		values := make([]any, 0, len(row))
		for _, value := range row {
			values = append(values, target.TruncateCell(target.FormatValue(value), cellLimit))
		}
		result.Rows = append(result.Rows, &values)
	}
	c.JSON(http.StatusOK, result)
}

// requireAccess verifies the user has any effective access on the connection.
func (h *Handler) requireAccess(c *gin.Context, user *ent.PlatformUser, id api.Id) (access.ConnectionAccess, bool) {
	level, err := access.ForConnection(c.Request.Context(), h.client, user.ID, int(id))
	if errors.Is(err, access.ErrNoAccess) {
		writeError(c, http.StatusForbidden, "forbidden", "no access to this database")
		return access.ConnectionAccess{}, false
	}
	if err != nil {
		h.fail(c, err, "check database access")
		return access.ConnectionAccess{}, false
	}
	_ = level
	return access.ConnectionAccess{Level: level}, true
}

// loadTargetDSN loads the enabled connection and decrypts its DSN.
func (h *Handler) loadTargetDSN(c *gin.Context, id api.Id) (*ent.DatabaseConnection, string, bool) {
	connection, err := h.client.DatabaseConnection.Query().
		Where(
			databaseconnection.ID(id),
			databaseconnection.IsEnabled(true),
		).
		Only(c.Request.Context())
	if ent.IsNotFound(err) {
		writeError(c, http.StatusNotFound, "not_found", "database connection not found")
		return nil, "", false
	}
	if err != nil {
		h.fail(c, err, "load database connection")
		return nil, "", false
	}
	password, err := h.targetPassword(connection)
	if err != nil {
		h.fail(c, err, "load target credentials")
		return nil, "", false
	}
	dsn, err := dsn.Config{
		Engine:       string(connection.Engine),
		Host:         connection.Host,
		Port:         connection.Port,
		DatabaseName: connection.DatabaseName,
		Username:     connection.Username,
		Password:     password,
		SSLMode:      string(connection.SslMode),
	}.DSN()
	if err != nil {
		h.fail(c, err, "build target dsn")
		return nil, "", false
	}
	return connection, dsn, true
}

// columnsSQL reads every table with its columns; run in a read-only transaction.
const columnsSQL = `
SELECT table_schema, table_name, column_name, data_type, is_nullable,
       ordinal_position, column_default, character_maximum_length
FROM information_schema.columns
WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
ORDER BY table_schema, table_name, ordinal_position`

// constraintsSQL reads constraint metadata per table/column.
const constraintsSQL = `
SELECT tc.table_schema, tc.table_name, tc.constraint_type, tc.constraint_name,
       kcu.column_name,
       ccu.table_name AS referenced_table,
       ccu.column_name AS referenced_column
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
  ON tc.constraint_name = kcu.constraint_name
 AND tc.constraint_schema = kcu.table_schema
LEFT JOIN information_schema.constraint_column_usage ccu
  ON tc.constraint_name = ccu.constraint_name
 AND tc.constraint_schema = ccu.constraint_schema
WHERE tc.table_schema NOT IN ('pg_catalog', 'information_schema')
ORDER BY tc.table_schema, tc.table_name, tc.constraint_name, kcu.ordinal_position`

// indexesSQL reads index definitions per table.
const indexesSQL = `
SELECT schemaname, tablename, indexname, indexdef
FROM pg_indexes
WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
ORDER BY schemaname, tablename, indexname`

const cellLimit = 4096

// mysqlColumnsSQL groups rows under the connected database as the single
// schema; the shape matches the Postgres projection.
const mysqlColumnsSQL = `
SELECT DATABASE() AS table_schema, table_name, column_name, data_type, is_nullable,
       ordinal_position, column_default, character_maximum_length
FROM information_schema.columns
WHERE table_schema = DATABASE()
ORDER BY table_name, ordinal_position`

// mysqlConstraintsSQL mirrors the Postgres constraint projection. MySQL 8
// key_column_usage carries referenced table/column names for foreign keys.
const mysqlConstraintsSQL = `
SELECT tc.table_schema, tc.table_name, tc.constraint_type, tc.constraint_name,
       kcu.column_name,
       kcu.referenced_table_name AS referenced_table,
       kcu.referenced_column_name AS referenced_column
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
  ON tc.constraint_name = kcu.constraint_name
 AND tc.constraint_schema = kcu.constraint_schema
 AND tc.table_name = kcu.table_name
WHERE tc.table_schema = DATABASE()
ORDER BY tc.table_name, tc.constraint_name, kcu.ordinal_position`

// mysqlIndexesSQL synthesizes a pg-style indexdef from
// information_schema.statistics; the PRIMARY pseudo-index duplicates the PK
// constraint and is skipped.
const mysqlIndexesSQL = `
SELECT table_schema, table_name, index_name,
       CONCAT('CREATE ', IF(MAX(non_unique)=0, 'UNIQUE ', ''), 'INDEX ', index_name,
              ' ON ', table_name, ' (', GROUP_CONCAT(column_name ORDER BY seq_in_index SEPARATOR ', '), ')') AS indexdef
FROM information_schema.statistics
WHERE table_schema = DATABASE() AND index_name <> 'PRIMARY'
GROUP BY table_schema, table_name, index_name
ORDER BY table_schema, table_name, index_name`

// buildSchema folds the flat columns, constraints, and indexes results into the API shape.
func buildSchema(cols []string, colRows, constraintRows, indexRows [][]any) api.DatabaseSchema {
	bySchema := map[string]*api.SchemaTables{}
	order := []string{}
	for _, row := range colRows {
		schemaName := toString(row[0])
		tableName := toString(row[1])
		st, ok := bySchema[schemaName]
		if !ok {
			st = &api.SchemaTables{Schema: schemaName, Tables: []api.SchemaTable{}}
			bySchema[schemaName] = st
			order = append(order, schemaName)
		}
		var table *api.SchemaTable
		table, st.Tables = findTable(st.Tables, tableName)
		column := api.SchemaColumn{
			Name:            toString(row[2]),
			Type:            toString(row[3]),
			IsNullable:      toString(row[4]) == "YES",
			OrdinalPosition: toInt(row[5]),
		}
		if row[6] != nil {
			column.ColumnDefault = &[]string{toString(row[6])}[0]
		}
		if row[7] != nil {
			column.CharacterMaximumLength = &[]int{toInt(row[7])}[0]
		}
		table.Columns = append(table.Columns, column)
	}
	for _, row := range constraintRows {
		schemaName := toString(row[0])
		tableName := toString(row[1])
		st, ok := bySchema[schemaName]
		if !ok {
			continue
		}
		var table *api.SchemaTable
		table, st.Tables = findTable(st.Tables, tableName)
		if table.Constraints == nil {
			empty := []api.SchemaConstraint{}
			table.Constraints = &empty
		}
		constraints := *table.Constraints
		var constraint *api.SchemaConstraint
		constraint, constraints = findConstraint(constraints, toString(row[3]))
		constraint.Type = toString(row[2])
		if len(constraint.Columns) == 0 || constraint.Columns[len(constraint.Columns)-1] != toString(row[4]) {
			constraint.Columns = append(constraint.Columns, toString(row[4]))
		}
		if toString(row[5]) != "" {
			constraint.ReferencedTable = &[]string{toString(row[5])}[0]
			constraint.ReferencedColumns = &[]string{toString(row[6])}
		}
		*table.Constraints = constraints
	}
	for _, row := range indexRows {
		schemaName := toString(row[0])
		tableName := toString(row[1])
		st, ok := bySchema[schemaName]
		if !ok {
			continue
		}
		var table *api.SchemaTable
		table, st.Tables = findTable(st.Tables, tableName)
		if table.Indexes == nil {
			empty := []api.SchemaIndex{}
			table.Indexes = &empty
		}
		def := toString(row[3])
		*table.Indexes = append(*table.Indexes, api.SchemaIndex{
			Name:     toString(row[2]),
			Columns:  parseIndexColumns(def),
			IsUnique: strings.HasPrefix(strings.TrimSpace(def), "CREATE UNIQUE INDEX"),
		})
	}
	schemas := make([]api.SchemaTables, 0, len(order))
	for _, name := range order {
		schemas = append(schemas, *bySchema[name])
	}
	return api.DatabaseSchema{Schemas: schemas}
}

// findConstraint returns the constraint with name, appending an empty one if
// absent. The caller must use the returned slice (append may reallocate).
func findConstraint(constraints []api.SchemaConstraint, name string) (*api.SchemaConstraint, []api.SchemaConstraint) {
	for i := range constraints {
		if constraints[i].Name == name {
			return &constraints[i], constraints
		}
	}
	constraints = append(constraints, api.SchemaConstraint{
		Name:    name,
		Type:    "UNKNOWN",
		Columns: []string{},
	})
	return &constraints[len(constraints)-1], constraints
}

// findTable returns the table with name, appending an empty one if absent.
// The caller must use the returned slice (append may reallocate).
func findTable(tables []api.SchemaTable, name string) (*api.SchemaTable, []api.SchemaTable) {
	for i := range tables {
		if tables[i].Name == name {
			return &tables[i], tables
		}
	}
	tables = append(tables, api.SchemaTable{Name: name, Columns: []api.SchemaColumn{}})
	return &tables[len(tables)-1], tables
}

// parseIndexColumns extracts the first top-level parenthesized column list
// from a pg_indexes indexdef ("CREATE INDEX x ON t USING btree (a, b DESC, lower(c))"
// -> ["a", "b DESC", "lower(c)"]). The column list is the FIRST top-level
// paren group; a partial-index predicate follows it.
func parseIndexColumns(def string) []string {
	start, depth, inQuote := -1, 0, false
	for i, r := range def {
		switch {
		case r == '\'':
			inQuote = !inQuote
		case inQuote:
		case r == '(':
			if depth == 0 {
				start = i
			}
			depth++
		case r == ')':
			depth--
			if depth == 0 {
				return splitIndexColumns(def[start+1 : i])
			}
		}
	}
	return []string{}
}

// splitIndexColumns splits a column list on top-level commas so quoted
// strings and function calls like lower(c) stay intact.
func splitIndexColumns(body string) []string {
	parts := []string{}
	depth, inQuote := 0, false
	partStart := 0
	for i, r := range body {
		switch {
		case r == '\'':
			inQuote = !inQuote
		case inQuote:
		case r == '(':
			depth++
		case r == ')':
			depth--
		case r == ',' && depth == 0:
			if name := strings.TrimSpace(body[partStart:i]); name != "" {
				parts = append(parts, name)
			}
			partStart = i + 1
		}
	}
	if name := strings.TrimSpace(body[partStart:]); name != "" {
		parts = append(parts, name)
	}
	if parts == nil {
		return []string{}
	}
	return parts
}

func toInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case []byte:
		n, _ := strconv.Atoi(string(t))
		return n
	case string:
		n, _ := strconv.Atoi(t)
		return n
	default:
		return 0
	}
}

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

var _ = time.RFC3339
