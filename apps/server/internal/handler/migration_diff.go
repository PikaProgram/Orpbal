package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"Orpbal/ent"
	"Orpbal/ent/migration"
	"Orpbal/ent/schemasnapshot"
	"Orpbal/internal/api"
	"Orpbal/internal/schema/build"
	"Orpbal/internal/target"
)

// captureSnapshot runs the schema crawl against the target and stores it as
// one phase of a migration's snapshot pair.
func (h *Handler) captureSnapshot(ctx context.Context, t *target.Target, migrationID int, phase schemasnapshot.Phase) error {
	schema, err := t.IntrospectSchema(ctx)
	if err != nil {
		return err
	}
	blob, err := json.Marshal(schema)
	if err != nil {
		return err
	}
	return h.client.SchemaSnapshot.Create().
		SetMigrationID(migrationID).
		SetPhase(phase).
		SetJSON(string(blob)).
		Exec(ctx)
}

// GetMigrationDiff returns the schema changes recorded between the before and
// after snapshots of one migration.
func (h *Handler) GetMigrationDiff(c *gin.Context, id api.Id, migrationId int) {
	if _, ok := h.authorizedTarget(c, id, ""); !ok {
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
	var haveAfter bool
	for _, snap := range snapshots {
		if snap.Phase == schemasnapshot.PhaseBefore {
			_ = json.Unmarshal([]byte(snap.JSON), &before)
		} else {
			_ = json.Unmarshal([]byte(snap.JSON), &after)
			haveAfter = true
		}
	}
	if haveAfter {
		diff.Changes = build.Diff(before, after)
	} else {
		// No after snapshot: nothing to diff. An empty list keeps the UI
		// honest instead of reporting every object "removed".
		diff.Changes = []api.SchemaChange{}
	}
	c.JSON(http.StatusOK, diff)
}
