package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"Orpbal/ent"
	"Orpbal/ent/databasegrant"
	"Orpbal/ent/migration"
	"Orpbal/ent/platformuser"
	"Orpbal/internal/api"
	"Orpbal/internal/target"
)

// ListMigrations returns the linear migration history for one database.
func (h *Handler) ListMigrations(c *gin.Context, id api.Id) {
	user := h.currentUser(c)
	if user == nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if _, ok := h.requireAccess(c, user, id); !ok {
		return
	}
	entries, err := h.client.Migration.Query().
		Where(migration.ConnectionID(int(id))).
		Order(ent.Asc(migration.FieldVersion)).
		All(c.Request.Context())
	if err != nil {
		h.fail(c, err, "list migrations")
		return
	}
	usernames := h.executorUsernames(c.Request.Context(), entries)
	result := make([]api.Migration, 0, len(entries))
	for _, entry := range entries {
		result = append(result, toAPIMigration(entry, usernames[entry.ExecutorID]))
	}
	c.JSON(http.StatusOK, result)
}

// CreateMigration stores a pending migration for the database.
func (h *Handler) CreateMigration(c *gin.Context, id api.Id, params api.CreateMigrationParams) {
	body := api.CreateMigrationRequest{}
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
	level, ok := h.requireAccess(c, user, id)
	if !ok {
		return
	}
	if level.Level != databasegrant.AccessLevelViewEdit {
		writeError(c, http.StatusForbidden, "forbidden", "view_edit access required")
		return
	}
	latest, err := h.client.Migration.Query().
		Where(migration.ConnectionID(int(id))).
		Order(ent.Desc(migration.FieldVersion)).
		First(c.Request.Context())
	if err != nil && !ent.IsNotFound(err) {
		h.fail(c, err, "find latest migration")
		return
	}
	version := 1
	if latest != nil {
		version = latest.Version + 1
	}
	entry, err := h.client.Migration.Create().
		SetConnectionID(int(id)).
		SetVersion(version).
		SetSQL(body.Sql).
		SetExecutorID(user.ID).
		SetStatus(migration.StatusPending).
		Save(c.Request.Context())
	if err != nil {
		h.fail(c, err, "create migration")
		return
	}
	c.JSON(http.StatusCreated, toAPIMigration(entry, h.executorUsernames(c.Request.Context(), []*ent.Migration{entry})[entry.ExecutorID]))
}

// ExecuteMigration runs a pending migration against the target in a
// transaction: commit on success, record failure on rollback.
func (h *Handler) ExecuteMigration(c *gin.Context, id api.Id, migrationId int, params api.ExecuteMigrationParams) {
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
	if entry.Status != migration.StatusPending {
		writeError(c, http.StatusConflict, "conflict", "migration already executed")
		return
	}
	_, dsnStr, ok := h.loadTargetDSN(c, id)
	if !ok {
		return
	}
	err = h.viewer.Execute(c.Request.Context(), dsnStr, entry.SQL)
	now := time.Now().UTC()
	status := migration.StatusSuccess
	if err != nil {
		status = migration.StatusFailed
	}
	entry, err = entry.Update().
		SetStatus(status).
		SetExecutedAt(now).
		Save(c.Request.Context())
	if err != nil {
		h.fail(c, err, "record migration status")
		return
	}
	if status == migration.StatusFailed {
		writeError(c, http.StatusBadGateway, "target_unreachable", "migration failed")
		return
	}
	c.JSON(http.StatusOK, toAPIMigration(entry, h.executorUsernames(c.Request.Context(), []*ent.Migration{entry})[entry.ExecutorID]))
}

// UpdateMigration edits the SQL of a still-pending migration.
func (h *Handler) UpdateMigration(c *gin.Context, id api.Id, migrationId int, params api.UpdateMigrationParams) {
	body := api.CreateMigrationRequest{}
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
	if entry.Status == migration.StatusSuccess {
		writeError(c, http.StatusConflict, "conflict", "migration already executed")
		return
	}
	if err := target.ReadableStatement(body.Sql); err != nil {
		writeFields(c, map[string]string{"sql": err.Error()})
		return
	}
	// Editing a failed migration re-opens it: status resets to pending so the
	// fixed SQL can be executed again. executed_at stays (non-nullable) but the
	// UI hides it for pending rows.
	update := entry.Update().SetSQL(body.Sql)
	if entry.Status == migration.StatusFailed {
		update.SetStatus(migration.StatusPending)
	}
	entry, err = update.Save(c.Request.Context())
	if err != nil {
		h.fail(c, err, "update migration")
		return
	}
	c.JSON(http.StatusOK, toAPIMigration(entry, h.executorUsernames(c.Request.Context(), []*ent.Migration{entry})[entry.ExecutorID]))
}

// toAPIMigration maps an ent migration to its API shape; username may be
// empty when the executor account no longer resolves.
func toAPIMigration(entry *ent.Migration, username string) api.Migration {
	return api.Migration{
		Id:               entry.ID,
		ConnectionId:     entry.ConnectionID,
		Version:          entry.Version,
		Sql:              entry.SQL,
		ExecutorId:       entry.ExecutorID,
		ExecutorUsername: username,
		Status:           api.MigrationStatus(entry.Status),
		ExecutedAt:       entry.ExecutedAt,
	}
}

// executorUsernames resolves the executor ids of a migration batch to usernames.
func (h *Handler) executorUsernames(ctx context.Context, entries []*ent.Migration) map[int]string {
	ids := make([]int, 0, len(entries))
	seen := map[int]bool{}
	for _, entry := range entries {
		if !seen[entry.ExecutorID] {
			seen[entry.ExecutorID] = true
			ids = append(ids, entry.ExecutorID)
		}
	}
	users, err := h.client.PlatformUser.Query().Where(platformuser.IDIn(ids...)).All(ctx)
	if err != nil {
		return map[int]string{}
	}
	names := make(map[int]string, len(users))
	for _, u := range users {
		names[u.ID] = u.Username
	}
	return names
}
