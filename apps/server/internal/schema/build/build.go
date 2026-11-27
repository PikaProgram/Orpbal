package build

import (
	"strconv"
	"strings"

	"Orpbal/internal/api"
)

// BuildSchema folds the flat columns, constraints, and indexes results into
// the API shape.
func BuildSchema(cols []string, colRows, constraintRows, indexRows [][]any) api.DatabaseSchema {
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
			Columns:  ParseIndexColumns(def),
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

// ParseIndexColumns extracts the first top-level parenthesized column list
// from a pg_indexes indexdef ("CREATE INDEX x ON t USING btree (a, b DESC, lower(c))"
// -> ["a", "b DESC", "lower(c)"]). The column list is the FIRST top-level
// paren group; a partial-index predicate follows it.
func ParseIndexColumns(def string) []string {
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
