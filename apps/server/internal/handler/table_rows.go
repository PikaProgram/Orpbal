package handler

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"Orpbal/ent/databasegrant"
	"Orpbal/internal/api"
)

// identifier is the strict shape accepted for table and column names in
// hand-built SQL. Values are always parameterized; identifiers cannot be.
var identifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// quoteIdent validates and double-quotes an identifier for interpolation.
func quoteIdent(value string) (string, error) {
	if !identifier.MatchString(value) {
		return "", fmt.Errorf("invalid identifier %q", value)
	}
	return `"` + value + `"`, nil
}

// ListTableCounts returns exact and estimated row counts for every table.
func (h *Handler) ListTableCounts(c *gin.Context, id api.Id) {
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
	_, rows, err := h.viewer.Query(c.Request.Context(), dsnStr, tableCountsSQL, 0)
	if err != nil {
		writeError(c, http.StatusBadGateway, "target_unreachable", err.Error())
		return
	}
	result := make([]api.TableCount, 0, len(rows))
	for _, row := range rows {
		entry := api.TableCount{
			Schema:   toString(row[0]),
			Table:    toString(row[1]),
			Columns:  int(toInt64(row[2])),
			Estimate: nil,
		}
		// reltuples < 0 means the planner has never analyzed the table.
		if v, ok := row[3].(float64); ok && v >= 0 {
			estimate := int(v)
			entry.Estimate = &estimate
		}
		result = append(result, entry)
	}
	c.JSON(http.StatusOK, result)
}

// tableCountsSQL lists every table with its column count and the planner's
// estimated row count in one pass.
const tableCountsSQL = `
SELECT n.nspname AS table_schema,
       c.relname AS table_name,
       (SELECT count(*) FROM information_schema.columns col
         WHERE col.table_schema = n.nspname AND col.table_name = c.relname) AS column_count,
       CASE WHEN c.reltuples >= 0 THEN c.reltuples ELSE NULL END AS estimate
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind = 'r'
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
ORDER BY n.nspname, c.relname`

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

// MutateRows applies one insert, update, or delete to a target table.
// Writes are gated to view_edit; the table must have a primary key for
// update and delete so every affected row is unambiguous.
func (h *Handler) MutateRows(c *gin.Context, id api.Id, _ api.MutateRowsParams) {
	body := api.MutateRowsRequest{}
	if !bindJSON(c, &body) {
		return
	}
	user := h.currentUser(c)
	if user == nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	level, ok := h.requireAccess(c, user, id)
	if !ok {
		return
	}
	if level.Level != databasegrant.AccessLevelViewEdit {
		writeError(c, http.StatusForbidden, "forbidden", "view_edit access required")
		return
	}
	_, dsnStr, ok := h.loadTargetDSN(c, id)
	if !ok {
		return
	}

	statement, args, err := buildMutation(body)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	affected, err := h.viewer.ExecuteArgs(c.Request.Context(), dsnStr, statement, args)
	if err != nil {
		writeError(c, http.StatusBadGateway, "target_unreachable", err.Error())
		return
	}
	c.JSON(http.StatusOK, api.MutationResult{Affected: int(affected)})
}

// buildMutation renders the parameterized SQL for a row mutation. The table
// and column names come from information_schema listings, but they are still
// validated against the identifier pattern before quoting.
func buildMutation(body api.MutateRowsRequest) (string, []any, error) {
	schemaName, err := quoteIdent(body.Schema)
	if err != nil {
		return "", nil, err
	}
	tableName, err := quoteIdent(body.Table)
	if err != nil {
		return "", nil, err
	}
	qualified := schemaName + "." + tableName

	switch body.Op {
	case api.Insert:
		if body.Values == nil || len(*body.Values) == 0 {
			return "", nil, fmt.Errorf("insert requires values")
		}
		values := *body.Values
		columns := make([]string, 0, len(values))
		placeholders := make([]string, 0, len(values))
		args := make([]any, 0, len(values))
		i := 1
		for _, name := range sortedKeys(values) {
			column, err := quoteIdent(name)
			if err != nil {
				return "", nil, err
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
			return "", nil, fmt.Errorf("update requires the primary key values")
		}
		if body.Values == nil || len(*body.Values) == 0 {
			return "", nil, fmt.Errorf("update requires at least one changed column")
		}
		values, pk := *body.Values, *body.Pk
		sets := make([]string, 0, len(values))
		args := make([]any, 0, len(values)+len(pk))
		i := 1
		for _, name := range sortedKeys(values) {
			column, err := quoteIdent(name)
			if err != nil {
				return "", nil, err
			}
			sets = append(sets, fmt.Sprintf("%s = $%d", column, i))
			args = append(args, values[name])
			i++
		}
		where, err := pkWhere(pk, &args, &i)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("UPDATE %s SET %s WHERE %s", qualified, strings.Join(sets, ", "), where), args, nil

	case api.Delete:
		if body.Pk == nil || len(*body.Pk) == 0 {
			return "", nil, fmt.Errorf("delete requires the primary key values")
		}
		pk := *body.Pk
		args := make([]any, 0, len(pk))
		i := 1
		where, err := pkWhere(pk, &args, &i)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("DELETE FROM %s WHERE %s", qualified, where), args, nil

	default:
		return "", nil, fmt.Errorf("unknown op %q", string(body.Op))
	}
}

// pkWhere renders "col = $n AND ..." for the primary key columns.
func pkWhere(pk map[string]any, args *[]any, i *int) (string, error) {
	parts := make([]string, 0, len(pk))
	for _, name := range sortedKeys(pk) {
		column, err := quoteIdent(name)
		if err != nil {
			return "", err
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
