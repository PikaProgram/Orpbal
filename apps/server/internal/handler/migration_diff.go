package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"Orpbal/ent"
	"Orpbal/ent/migration"
	"Orpbal/ent/migrationexecution"
	"Orpbal/ent/schemasnapshot"
	"Orpbal/internal/api"
	"Orpbal/internal/schema/build"
	"Orpbal/internal/target"
)

// GetMigrationDiff diffs the before/after schema snapshots of the
// migration's latest successful execution.
func (h *Handler) GetMigrationDiff(c *gin.Context, id api.Id, migrationId int) {
	if _, ok := h.authorizedTarget(c, id, ""); !ok {
		return
	}
	entry, err := h.client.Migration.Query().
		Where(migration.ID(migrationId), migration.ConnectionID(int(id))).
		WithExecutions(func(q *ent.MigrationExecutionQuery) {
			q.
				Where(migrationexecution.StatusEQ(migrationexecution.StatusSuccess)).
				Order(ent.Desc(migrationexecution.FieldCreatedAt)).
				WithSnapshots()
		}).
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
	var best *ent.MigrationExecution
	for _, e := range entry.Edges.Executions {
		if best == nil {
			best = e
			continue
		}
		if e.CreatedAt.After(best.CreatedAt) {
			best = e
		}
	}
	if best == nil {
		c.JSON(http.StatusOK, diff)
		return
	}
	var before, after api.DatabaseSchema
	var haveAfter bool
	for _, snap := range best.Edges.Snapshots {
		switch snap.Phase {
		case schemasnapshot.PhaseBefore:
			_ = json.Unmarshal([]byte(snap.JSON), &before)
		case schemasnapshot.PhaseAfter:
			_ = json.Unmarshal([]byte(snap.JSON), &after)
			haveAfter = true
		}
	}
	if haveAfter {
		diff.Changes = build.Diff(before, after)
	}
	c.JSON(http.StatusOK, diff)
}

// captureSnapshot runs the schema crawl against the target and stores it as
// one phase of an execution's snapshot pair.
func (h *Handler) captureSnapshot(ctx context.Context, t *target.Target, migrationID, executionID int, phase schemasnapshot.Phase) error {
	schema, err := t.IntrospectSchema(ctx)
	if err != nil {
		return err
	}
	blob, err := json.Marshal(schema)
	if err != nil {
		return err
	}
	create := h.client.SchemaSnapshot.Create().
		SetMigrationID(migrationID).
		SetPhase(phase).
		SetJSON(string(blob))
	if executionID != 0 {
		create.SetExecutionID(executionID)
	}
	return create.Exec(ctx)
}
