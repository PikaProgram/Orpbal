package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"Orpbal/ent"
	"Orpbal/ent/databasegrant"
	"Orpbal/ent/migration"
	"Orpbal/ent/migrationapproval"
	"Orpbal/ent/migrationattempt"
	"Orpbal/ent/migrationexecution"
	"Orpbal/ent/platformuser"
	"Orpbal/internal/access"
	"Orpbal/internal/api"
	"Orpbal/internal/service"
)

// ListMigrations returns the migration history for one database, newest
// executions first. hide_rollback hides execution pairs cancelled by a later
// successful rollback; failed attempts always stay visible.
func (h *Handler) ListMigrations(c *gin.Context, id api.Id, params api.ListMigrationsParams) {
	if _, ok := h.authorizedTarget(c, id, ""); !ok {
		return
	}
	entries, err := h.client.Migration.Query().
		Where(migration.ConnectionID(int(id))).
		WithExecutions(func(q *ent.MigrationExecutionQuery) {
			q.WithActor().WithSnapshots().WithAttempts(func(aq *ent.MigrationAttemptQuery) {
				aq.Order(ent.Asc(migrationattempt.FieldAttempt))
			}).Order(ent.Desc(migrationexecution.FieldCreatedAt))
		}).
		WithApprovals().
		Order(ent.Asc(migration.FieldVersion)).
		All(c.Request.Context())
	if err != nil {
		h.fail(c, err, "list migrations")
		return
	}
	hidden := hiddenByRollback(entries)
	result := make([]api.Migration, 0, len(entries))
	for _, entry := range entries {
		result = append(result, h.toAPIMigration(c.Request.Context(), entry, hidden, params.HideRollback != nil && *params.HideRollback))
	}
	c.JSON(http.StatusOK, result)
}

// hiddenByRollback collects execution ids cancelled by a later successful
// rollback plus those rollback ids themselves; failed attempts never hide.
func hiddenByRollback(entries []*ent.Migration) map[int]bool {
	var executions []*ent.MigrationExecution
	for _, entry := range entries {
		executions = append(executions, entry.Edges.Executions...)
	}
	rollbackOf := map[int]int{} // up/forward execution id -> rollback id
	for _, e := range executions {
		if e.Kind == migrationexecution.KindRollback && e.Status == migrationexecution.StatusSuccess && e.RevertedExecutionID != nil {
			rollbackOf[*e.RevertedExecutionID] = e.ID
		}
	}
	hidden := map[int]bool{}
	for revertedID, rollbackID := range rollbackOf {
		hidden[revertedID] = true
		hidden[rollbackID] = true
	}
	return hidden
}

// CreateMigration stores a new draft migration for the database.
func (h *Handler) CreateMigration(c *gin.Context, id api.Id, params api.CreateMigrationParams) {
	body, ok := h.migrationBody(c)
	if !ok {
		return
	}
	if _, ok := h.authorizedTarget(c, id, databasegrant.AccessLevelViewEdit); !ok {
		return
	}
	user := h.currentUser(c)
	entry, err := service.CreateMigration(c.Request.Context(), h.client, user.ID, int(id), body)
	h.writeMigrationResult(c, entry, err, http.StatusCreated)
}

// UpdateMigration edits a migration that is still a draft.
func (h *Handler) UpdateMigration(c *gin.Context, id api.Id, migrationId int, params api.UpdateMigrationParams) {
	body, ok := h.migrationBody(c)
	if !ok {
		return
	}
	if _, ok := h.authorizedTarget(c, id, databasegrant.AccessLevelViewEdit); !ok {
		return
	}
	user := h.currentUser(c)
	entry, err := service.UpdateMigration(c.Request.Context(), h.client, user.ID, int(id), migrationId, body)
	h.writeMigrationResult(c, entry, err, http.StatusOK)
}

// DeleteMigration removes a migration that is still a draft.
func (h *Handler) DeleteMigration(c *gin.Context, id api.Id, migrationId int, params api.DeleteMigrationParams) {
	if _, ok := h.authorizedTarget(c, id, databasegrant.AccessLevelViewEdit); !ok {
		return
	}
	user := h.currentUser(c)
	err := service.DeleteMigration(c.Request.Context(), h.client, user.ID, int(id), migrationId)
	h.writeServiceError(c, err)
	if err == nil {
		c.Status(http.StatusNoContent)
	}
}

// SubmitMigration moves a draft migration into review.
func (h *Handler) SubmitMigration(c *gin.Context, id api.Id, migrationId int, params api.SubmitMigrationParams) {
	if _, ok := h.authorizedTarget(c, id, databasegrant.AccessLevelViewEdit); !ok {
		return
	}
	user := h.currentUser(c)
	entry, err := service.Submit(c.Request.Context(), h.client, user.ID, int(id), migrationId)
	h.writeMigrationResult(c, entry, err, http.StatusOK)
}

// RequestRollback queues a rollback of the migration's active execution.
func (h *Handler) RequestRollback(c *gin.Context, id api.Id, migrationId int, params api.RequestRollbackParams) {
	if _, ok := h.authorizedTarget(c, id, databasegrant.AccessLevelViewEdit); !ok {
		return
	}
	user := h.currentUser(c)
	entry, err := service.RequestRollback(c.Request.Context(), h.client, user.ID, int(id), migrationId)
	h.writeMigrationResult(c, entry, err, http.StatusOK)
}

// RequestForward re-applies a rolled-back migration or retries a failed one.
func (h *Handler) RequestForward(c *gin.Context, id api.Id, migrationId int, params api.RequestForwardParams) {
	if _, ok := h.authorizedTarget(c, id, databasegrant.AccessLevelViewEdit); !ok {
		return
	}
	user := h.currentUser(c)
	entry, err := service.RequestForward(c.Request.Context(), h.client, user.ID, int(id), migrationId)
	h.writeMigrationResult(c, entry, err, http.StatusOK)
}

// RequestBatchRevert queues compensation for a partial batch run.
func (h *Handler) RequestBatchRevert(c *gin.Context, id api.Id, migrationId int, params api.RequestBatchRevertParams) {
	if _, ok := h.authorizedTarget(c, id, databasegrant.AccessLevelViewEdit); !ok {
		return
	}
	body := api.BatchRevertRequest{}
	if !bindJSON(c, &body) {
		return
	}
	user := h.currentUser(c)
	entry, err := service.RequestBatchRevert(c.Request.Context(), h.client, user.ID, int(id), migrationId, body.ExecutionId, body.SqlDown, body.Template)
	h.writeMigrationResult(c, entry, err, http.StatusOK)
}

// migrationBody decodes and validates the shared create/update body.
func (h *Handler) migrationBody(c *gin.Context) (*service.CreateBody, bool) {
	req := api.CreateMigrationRequest{}
	if !bindJSON(c, &req) {
		return nil, false
	}
	body := &service.CreateBody{
		SqlUp:              req.SqlUp,
		SqlDown:            req.SqlDown,
		Irreversible:       req.Irreversible != nil && *req.Irreversible,
		IrreversibleReason: req.IrreversibleReason,
	}
	if req.Batch != nil {
		body.Batch = &service.BatchBody{
			BatchSize:    req.Batch.BatchSize,
			CursorColumn: req.Batch.CursorColumn,
			BatchTable:   derefStr(req.Batch.BatchTable),
			Template:     req.Batch.Template,
			MinId:        req.Batch.MinId,
			MaxId:        req.Batch.MaxId,
		}
	}
	return body, true
}

// derefStr reads an optional string field.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// writeMigrationResult maps a service result or error to a response.
func (h *Handler) writeMigrationResult(c *gin.Context, entry *ent.Migration, err error, okStatus int) {
	if h.writeServiceError(c, err) {
		return
	}
	loaded, err := h.client.Migration.Query().
		Where(migration.ID(entry.ID)).
		WithExecutions(func(q *ent.MigrationExecutionQuery) {
			q.WithActor().WithSnapshots().WithAttempts(func(aq *ent.MigrationAttemptQuery) {
				aq.Order(ent.Asc(migrationattempt.FieldAttempt))
			}).Order(ent.Desc(migrationexecution.FieldCreatedAt))
		}).
		WithApprovals().
		Only(c.Request.Context())
	if err != nil {
		h.fail(c, err, "load migration")
		return
	}
	c.JSON(okStatus, h.toAPIMigration(c.Request.Context(), loaded, nil, false))
}

// writeServiceError maps service sentinel errors to status codes; returns
// true when a response was written.
func (h *Handler) writeServiceError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, service.ErrNotFound):
		writeError(c, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, service.ErrValidation):
		writeError(c, http.StatusBadRequest, "validation_failed", err.Error())
	case errors.Is(err, service.ErrNoLeaderGroup):
		writeError(c, http.StatusConflict, "no_leader", "group has no leader")
	case errors.Is(err, service.ErrNotLeader):
		writeError(c, http.StatusForbidden, "forbidden", "only the current group leader can decide approvals")
	case errors.Is(err, service.ErrVersionOrder):
		writeError(c, http.StatusConflict, "version_order", err.Error())
	case errors.Is(err, service.ErrConflict):
		writeError(c, http.StatusConflict, "conflict", err.Error())
	default:
		h.fail(c, err, "migration service")
	}
	return true
}

// ListMyApprovals lists approvals the caller can act on or has decided.
func (h *Handler) ListMyApprovals(c *gin.Context, params api.ListMyApprovalsParams) {
	user := h.currentUser(c)
	if user == nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var status *migrationapproval.Status
	if params.Status != nil {
		s := migrationapproval.Status(*params.Status)
		status = &s
	}
	rows, err := service.MyApprovals(c.Request.Context(), h.client, user.ID, status)
	if err != nil {
		h.fail(c, err, "list approvals")
		return
	}
	result := make([]api.Approval, 0, len(rows))
	for _, a := range rows {
		result = append(result, h.toAPIApproval(c.Request.Context(), a))
	}
	c.JSON(http.StatusOK, result)
}

// ApproveMigration records the current group leader's approval.
func (h *Handler) ApproveMigration(c *gin.Context, approvalId int, params api.ApproveMigrationParams) {
	user := h.currentUser(c)
	if user == nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	migration, exec, job, err := service.Approve(c.Request.Context(), h.client, user.ID, approvalId)
	if h.writeServiceError(c, err) {
		return
	}
	_ = exec
	resp := map[string]any{"migration_id": migration.ID, "status": string(migration.Status)}
	if job != nil {
		resp["job_id"] = job.ID
	}
	c.JSON(http.StatusOK, resp)
}

// RejectMigration records the current group leader's rejection.
func (h *Handler) RejectMigration(c *gin.Context, approvalId int, params api.RejectMigrationParams) {
	user := h.currentUser(c)
	if user == nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	body := api.RejectRequest{}
	_ = c.ShouldBindJSON(&body) // body optional
	entry, err := service.Reject(c.Request.Context(), h.client, user.ID, approvalId, body.Comment)
	if h.writeServiceError(c, err) {
		return
	}
	c.JSON(http.StatusOK, map[string]any{"migration_id": entry.ID, "status": string(entry.Status)})
}

// GetJob returns one queue row with its execution; any access level on the
// connection may read it.
func (h *Handler) GetJob(c *gin.Context, jobId int) {
	user := h.currentUser(c)
	if user == nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	job, err := service.GetJob(c.Request.Context(), h.client, jobId)
	if ent.IsNotFound(err) {
		writeError(c, http.StatusNotFound, "not_found", "job not found")
		return
	}
	if err != nil {
		h.fail(c, err, "load job")
		return
	}
	if _, err := access.ForConnection(c.Request.Context(), h.client, user.ID, job.ConnectionID); err != nil {
		writeError(c, http.StatusForbidden, "forbidden", "no access to this database")
		return
	}
	c.JSON(http.StatusOK, h.toAPIJob(c.Request.Context(), job))
}

// CancelJob cancels a queued job or flags a running one for cancellation.
func (h *Handler) CancelJob(c *gin.Context, jobId int, params api.CancelJobParams) {
	user := h.currentUser(c)
	if user == nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	job, err := service.GetJob(c.Request.Context(), h.client, jobId)
	if ent.IsNotFound(err) {
		writeError(c, http.StatusNotFound, "not_found", "job not found")
		return
	}
	if err != nil {
		h.fail(c, err, "load job")
		return
	}
	if _, err := access.ForConnection(c.Request.Context(), h.client, user.ID, job.ConnectionID); err != nil {
		writeError(c, http.StatusForbidden, "forbidden", "no access to this database")
		return
	}
	updated, err := service.CancelJob(c.Request.Context(), h.client, jobId)
	if h.writeServiceError(c, err) {
		return
	}
	c.JSON(http.StatusOK, toAPIJobSimple(updated))
}

// userNames resolves user ids to usernames in one query.
func (h *Handler) userNames(ctx context.Context, ids ...int) map[int]string {
	names := map[int]string{}
	if len(ids) == 0 {
		return names
	}
	users, err := h.client.PlatformUser.Query().Where(platformuser.IDIn(ids...)).All(ctx)
	if err != nil {
		return names
	}
	for _, u := range users {
		names[u.ID] = u.Username
	}
	return names
}

// toAPIJobSimple maps a bare job row to the API shape.
func toAPIJobSimple(job *ent.MigrationJob) api.Job {
	out := api.Job{
		Id:           job.ID,
		ExecutionId:  job.ExecutionID,
		ConnectionId: job.ConnectionID,
		MigrationId:  0,
		Status:       api.JobStatus(job.Status),
		CreatedAt:    job.CreatedAt,
		Attempts:     &job.Attempts,
	}
	return out
}

// toAPIExecution maps an execution to the API shape, including its
// append-only per-attempt history.
func (h *Handler) toAPIExecution(ctx context.Context, e *ent.MigrationExecution, snapshots []api.SchemaSnapshotInfo) api.Execution {
	ids := []int{e.ActorID}
	for _, a := range e.Edges.Attempts {
		ids = append(ids, a.ActorID)
	}
	names := h.userNames(ctx, ids...)
	out := api.Execution{
		Id:            e.ID,
		MigrationId:   e.MigrationID,
		ConnectionId:  e.ConnectionID,
		Kind:          api.ExecutionKind(e.Kind),
		Status:        api.MigrationStatus(e.Status),
		ActorId:       e.ActorID,
		ActorUsername: names[e.ActorID],
		CreatedAt:     e.CreatedAt,
		BatchesDone:   &e.BatchesDone,
		RowsDone:      &e.RowsDone,
		Snapshots:     &snapshots,
	}
	if e.SQLExecuted != "" {
		out.SqlExecuted = &e.SQLExecuted
	}
	if e.Error != "" {
		out.Error = &e.Error
	}
	out.RevertedExecutionId = e.RevertedExecutionID
	out.BatchCursor = e.BatchCursor
	out.FinishedAt = e.FinishedAt
	attempts := make([]api.MigrationAttempt, 0, len(e.Edges.Attempts))
	for _, a := range e.Edges.Attempts {
		attempts = append(attempts, toAPIAttempt(names, a))
	}
	out.Attempts = &attempts
	return out
}

// toAPIAttempt maps one append-only attempt row to the API shape.
func toAPIAttempt(names map[int]string, a *ent.MigrationAttempt) api.MigrationAttempt {
	out := api.MigrationAttempt{
		Id:           a.ID,
		MigrationId:  a.MigrationID,
		ExecutionId:  a.ExecutionID,
		ConnectionId: a.ConnectionID,
		Attempt:      a.Attempt,
		Kind:         api.ExecutionKind(a.Kind),
		Status:       api.MigrationAttemptStatus(a.Status),
		ActorId:      a.ActorID,
		BatchesDone:  a.BatchesDone,
		RowsDone:     a.RowsDone,
		StartedAt:    a.StartedAt,
	}
	if name := names[a.ActorID]; name != "" {
		out.ActorUsername = &name
	}
	if a.WorkerID != "" {
		out.WorkerId = &a.WorkerID
	}
	if a.SQLExecuted != "" {
		out.SqlExecuted = &a.SQLExecuted
	}
	if a.Error != "" {
		out.Error = &a.Error
	}
	out.BatchCursor = a.BatchCursor
	out.FinishedAt = a.FinishedAt
	return out
}

// toAPIMigration maps a migration with eager-loaded edges to the API shape.
func (h *Handler) toAPIMigration(ctx context.Context, entry *ent.Migration, hidden map[int]bool, hideRollback bool) api.Migration {
	authorNames := h.userNames(ctx, entry.AuthorID)
	out := api.Migration{
		Id:             entry.ID,
		ConnectionId:   entry.ConnectionID,
		Version:        entry.Version,
		SqlUp:          entry.SQLUp,
		Status:         api.MigrationStatus(entry.Status),
		AuthorId:       entry.AuthorID,
		AuthorUsername: authorNames[entry.AuthorID],
		Executions:     []api.Execution{},
		Approvals:      []api.Approval{},
	}
	if entry.SQLDown != "" {
		out.SqlDown = &entry.SQLDown
	}
	out.Irreversible = entry.Irreversible
	if entry.IrreversibleReason != "" {
		out.IrreversibleReason = &entry.IrreversibleReason
	}
	if entry.BatchSize > 0 {
		table := entry.BatchTable
		out.Batch = &api.BatchConfig{
			BatchSize:    entry.BatchSize,
			BatchTable:   &table,
			CursorColumn: entry.CursorColumn,
			Template:     entry.Template,
			MinId:        entry.MinID,
			MaxId:        entry.MaxID,
		}
	}
	for _, e := range entry.Edges.Executions {
		if hideRollback && hidden != nil && hidden[e.ID] {
			continue
		}
		snapshots := []api.SchemaSnapshotInfo{}
		for _, s := range e.Edges.Snapshots {
			info := api.SchemaSnapshotInfo{Phase: api.SchemaSnapshotInfoPhase(s.Phase)}
			info.CapturedAt = nil
			snapshots = append(snapshots, info)
		}
		out.Executions = append(out.Executions, h.toAPIExecution(ctx, e, snapshots))
	}
	for _, a := range entry.Edges.Approvals {
		out.Approvals = append(out.Approvals, h.toAPIApproval(ctx, a))
	}
	return out
}

// toAPIApproval maps an approval row to the API shape.
func (h *Handler) toAPIApproval(ctx context.Context, a *ent.MigrationApproval) api.Approval {
	leaderNames := h.userNames(ctx, a.LeaderID)
	groupName := ""
	if a.Edges.Group != nil {
		groupName = a.Edges.Group.Name
	}
	out := api.Approval{
		Id:             a.ID,
		MigrationId:    a.MigrationID,
		GroupId:        a.GroupID,
		GroupName:      groupName,
		LeaderId:       a.LeaderID,
		LeaderUsername: leaderNames[a.LeaderID],
		Status:         api.ApprovalStatus(a.Status),
		CreatedAt:      a.CreatedAt,
	}
	out.DecidedById = a.DecidedByID
	out.DecidedAt = a.DecidedAt
	if a.Comment != "" {
		out.Comment = &a.Comment
	}
	return out
}

// toAPIJob maps a job with its execution edge to the API shape.
func (h *Handler) toAPIJob(ctx context.Context, job *ent.MigrationJob) api.Job {
	out := toAPIJobSimple(job)
	out.Attempts = &job.Attempts
	out.StartedAt = job.StartedAt
	out.FinishedAt = job.FinishedAt
	out.BatchCursor = job.BatchCursor
	out.BatchesDone = &job.BatchesDone
	out.RowsDone = &job.RowsDone
	if job.Error != "" {
		out.Error = &job.Error
	}
	if job.Edges.Execution != nil {
		out.MigrationId = job.Edges.Execution.MigrationID
	}
	return out
}
