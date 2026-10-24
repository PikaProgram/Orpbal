package handler

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"Orpbal/ent"
	"Orpbal/ent/migration"
	"Orpbal/ent/schemasnapshot"
	"Orpbal/internal/api"
)

// captureSnapshot runs the schema crawl against the target and stores it as
// one phase of a migration's snapshot pair.
func (h *Handler) captureSnapshot(c *gin.Context, dsn string, migrationID int, phase schemasnapshot.Phase) error {
	colCols, colRows, err := h.viewer.Query(c.Request.Context(), dsn, columnsSQL, 0)
	if err != nil {
		return err
	}
	_, constraintRows, err := h.viewer.Query(c.Request.Context(), dsn, constraintsSQL, 0)
	if err != nil {
		return err
	}
	_, indexRows, err := h.viewer.Query(c.Request.Context(), dsn, indexesSQL, 0)
	if err != nil {
		return err
	}
	schema := buildSchema(colCols, colRows, constraintRows, indexRows)
	blob, err := json.Marshal(schema)
	if err != nil {
		return err
	}
	return h.client.SchemaSnapshot.Create().
		SetMigrationID(migrationID).
		SetPhase(phase).
		SetJSON(string(blob)).
		Exec(c.Request.Context())
}

// GetMigrationDiff returns the schema changes recorded between the before and
// after snapshots of one migration.
func (h *Handler) GetMigrationDiff(c *gin.Context, id api.Id, migrationId int) {
	user := h.currentUser(c)
	if user == nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if _, ok := h.requireAccess(c, user, id); !ok {
		return
	}
	entry, err := h.client.Migration.Query().
		Where(migration.ID(migrationId), migration.ConnectionID(int(id))).
		Only(c.Request.Context())
	if ent.IsNotFound(err) {
		writeError(c, http.StatusNotFound, "not_found", "migration not found")
		return
	}
	if err != nil {
		h.fail(c, err, "load migration")
		return
	}

	diff := api.MigrationDiff{
		MigrationId: entry.ID,
		Status:      api.MigrationDiffStatus(entry.Status),
		Changes:     []api.SchemaChange{},
	}
	if entry.Status != migration.StatusSuccess {
		// Pending and failed migrations have no "after" state to diff.
		c.JSON(http.StatusOK, diff)
		return
	}

	snapshots, err := entry.QuerySnapshots().All(c.Request.Context())
	if err != nil {
		h.fail(c, err, "load snapshots")
		return
	}
	var before, after api.DatabaseSchema
	for _, snap := range snapshots {
		if snap.Phase == schemasnapshot.PhaseBefore {
			_ = json.Unmarshal([]byte(snap.JSON), &before)
		} else {
			_ = json.Unmarshal([]byte(snap.JSON), &after)
		}
	}
	diff.Changes = schemaDiff(before, after)
	c.JSON(http.StatusOK, diff)
}

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

// schemaDiff walks both snapshots and emits added, removed, and changed
// entries for tables, columns, constraints, and indexes.
func schemaDiff(before, after api.DatabaseSchema) []api.SchemaChange {
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
