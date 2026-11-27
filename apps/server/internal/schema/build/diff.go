package build

import (
	"sort"
	"strings"

	"Orpbal/internal/api"
)

// tableKey indexes tables by qualified name across the two snapshots.
func tableKey(schemaName, tableName string) string { return schemaName + "." + tableName }

// constraintKey identifies a constraint inside one table.
func constraintKey(t api.SchemaConstraint) string { return t.Type + " " + t.Name }

// columnSignature renders a column's definition so type or nullability
// changes register as "changed".
func columnSignature(c api.SchemaColumn) string {
	if c.IsNullable {
		return c.Type + " NULL"
	}
	return c.Type + " NOT NULL"
}

// indexSignature renders an index definition for comparison.
func indexSignature(i api.SchemaIndex) string {
	prefix := ""
	if i.IsUnique {
		prefix = "UNIQUE "
	}
	return prefix + i.Name + " (" + strings.Join(i.Columns, ", ") + ")"
}

// strPtr returns a pointer to s, for optional API fields.
// shortcut: Go 1.26 new(expr) would replace this; upgrade when go.mod bumps.
func strPtr(s string) *string { return &s }

// Diff walks both snapshots and emits added, removed, and changed entries for
// tables, columns, constraints, and indexes.
func Diff(before, after api.DatabaseSchema) []api.SchemaChange {
	changes := []api.SchemaChange{}

	beforeTables := map[string]api.SchemaTable{}
	for _, s := range before.Schemas {
		for _, t := range s.Tables {
			beforeTables[tableKey(s.Schema, t.Name)] = t
		}
	}
	afterTables := map[string]api.SchemaTable{}
	for _, s := range after.Schemas {
		for _, t := range s.Tables {
			afterTables[tableKey(s.Schema, t.Name)] = t
		}
	}

	keys := make([]string, 0, len(beforeTables)+len(afterTables))
	for key := range beforeTables {
		keys = append(keys, key)
	}
	for key := range afterTables {
		if _, seen := beforeTables[key]; !seen {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	for _, key := range keys {
		beforeTable, hadBefore := beforeTables[key]
		afterTable, hasAfter := afterTables[key]

		if !hadBefore {
			changes = append(changes, api.SchemaChange{
				ObjectType: "table", ObjectId: key, Action: "added",
			})
		} else if !hasAfter {
			changes = append(changes, api.SchemaChange{
				ObjectType: "table", ObjectId: key, Action: "removed",
			})
			continue
		}

		// Columns.
		beforeCols := map[string]api.SchemaColumn{}
		for _, col := range beforeTable.Columns {
			beforeCols[col.Name] = col
		}
		afterCols := map[string]api.SchemaColumn{}
		for _, col := range afterTable.Columns {
			afterCols[col.Name] = col
		}
		for _, col := range afterTable.Columns {
			old, ok := beforeCols[col.Name]
			if !ok {
				changes = append(changes, api.SchemaChange{
					ObjectType: "column", ObjectId: key + "." + col.Name,
					Action: "added", Detail: strPtr(columnSignature(col)),
				})
			} else if columnSignature(old) != columnSignature(col) {
				changes = append(changes, api.SchemaChange{
					ObjectType: "column", ObjectId: key + "." + col.Name,
					Action: "changed", Detail: strPtr(columnSignature(old) + " → " + columnSignature(col)),
				})
			}
		}
		for _, col := range beforeTable.Columns {
			if _, ok := afterCols[col.Name]; !ok {
				changes = append(changes, api.SchemaChange{
					ObjectType: "column", ObjectId: key + "." + col.Name,
					Action: "removed", Detail: strPtr(columnSignature(col)),
				})
			}
		}

		// Constraints.
		beforeConstraints := map[string]string{}
		if beforeTable.Constraints != nil {
			for _, con := range *beforeTable.Constraints {
				beforeConstraints[constraintKey(con)] = strings.Join(con.Columns, ", ")
			}
		}
		if afterTable.Constraints != nil {
			for _, con := range *afterTable.Constraints {
				id := constraintKey(con)
				cols := strings.Join(con.Columns, ", ")
				old, ok := beforeConstraints[id]
				if !ok {
					changes = append(changes, api.SchemaChange{
						ObjectType: "constraint", ObjectId: key + "." + con.Name,
						Action: "added", Detail: strPtr(id + " (" + cols + ")"),
					})
				} else if old != cols {
					changes = append(changes, api.SchemaChange{
						ObjectType: "constraint", ObjectId: key + "." + con.Name,
						Action: "changed", Detail: strPtr(old + " → " + cols),
					})
				}
				delete(beforeConstraints, id)
			}
		}
		for id, cols := range beforeConstraints {
			changes = append(changes, api.SchemaChange{
				ObjectType: "constraint", ObjectId: key + "." + id,
				Action: "removed", Detail: strPtr(cols),
			})
		}

		// Indexes.
		beforeIndexes := map[string]string{}
		if beforeTable.Indexes != nil {
			for _, idx := range *beforeTable.Indexes {
				beforeIndexes[idx.Name] = indexSignature(idx)
			}
		}
		if afterTable.Indexes != nil {
			for _, idx := range *afterTable.Indexes {
				old, ok := beforeIndexes[idx.Name]
				if !ok {
					changes = append(changes, api.SchemaChange{
						ObjectType: "index", ObjectId: key + "." + idx.Name,
						Action: "added", Detail: strPtr(indexSignature(idx)),
					})
				} else if old != indexSignature(idx) {
					changes = append(changes, api.SchemaChange{
						ObjectType: "index", ObjectId: key + "." + idx.Name,
						Action: "changed", Detail: strPtr(old + " → " + indexSignature(idx)),
					})
				}
				delete(beforeIndexes, idx.Name)
			}
		}
		for name, sig := range beforeIndexes {
			changes = append(changes, api.SchemaChange{
				ObjectType: "index", ObjectId: key + "." + name,
				Action: "removed", Detail: strPtr(sig),
			})
		}
	}

	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Action != changes[j].Action {
			return actionRank(changes[i].Action) < actionRank(changes[j].Action)
		}
		return changes[i].ObjectId < changes[j].ObjectId
	})
	return changes
}

func actionRank(action api.SchemaChangeAction) int {
	switch action {
	case "added":
		return 0
	case "changed":
		return 1
	default:
		return 2
	}
}
