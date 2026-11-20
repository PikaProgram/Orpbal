package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"Orpbal/ent"
	"Orpbal/ent/databasegrant"
	"Orpbal/ent/groupmembership"
	"Orpbal/ent/migration"
	"Orpbal/ent/migrationapproval"
	"Orpbal/ent/migrationexecution"
	"Orpbal/ent/migrationjob"
	"Orpbal/internal/tracing"
)

// identifier is the strict shape for a batch cursor column; it will be
// interpolated into worker SQL, so it cannot be free-form.
var identifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// Sentinel errors handlers map to status codes.
var (
	ErrNotFound      = errors.New("not found")
	ErrNoAccess      = errors.New("no access")
	ErrConflict      = errors.New("conflict")
	ErrValidation    = errors.New("validation")
	ErrNoLeader      = errors.New("group has no leader")
	ErrNotLeader     = errors.New("caller is not the group leader")
	ErrNoLeaderGroup = errors.New("leaderless group")
)

// ValidateWrite confirms the shared create/update body: sql_up required,
// irreversible needs a reason, and a partial batch config is rejected.
func ValidateWrite(body *CreateBody) error {
	if strings.TrimSpace(body.SqlUp) == "" {
		return fmt.Errorf("%w: sql_up is required", ErrValidation)
	}
	irrev := body.Irreversible
	if irrev && strings.TrimSpace(deref(body.IrreversibleReason)) == "" {
		return fmt.Errorf("%w: irreversible_reason is required when irreversible", ErrValidation)
	}
	if body.Batch == nil {
		return nil
	}
	b := body.Batch
	if strings.TrimSpace(b.Template) == "" {
		return fmt.Errorf("%w: batch.template is required", ErrValidation)
	}
	if b.BatchSize <= 0 {
		return fmt.Errorf("%w: batch.batch_size must be positive", ErrValidation)
	}
	if strings.TrimSpace(b.BatchTable) == "" {
		return fmt.Errorf("%w: batch.batch_table is required", ErrValidation)
	}
	if !identifier.MatchString(b.CursorColumn) {
		return fmt.Errorf("%w: batch.cursor_column must be a plain identifier", ErrValidation)
	}
	if !identifier.MatchString(b.BatchTable) {
		return fmt.Errorf("%w: batch.batch_table must be a plain identifier", ErrValidation)
	}
	if b.MinId != nil && b.MaxId != nil && *b.MinId > *b.MaxId {
		return fmt.Errorf("%w: batch.min_id must not exceed batch.max_id", ErrValidation)
	}
	return nil
}

// CreateBody is the decoded request shared by create and update.
type CreateBody struct {
	SqlUp              string
	SqlDown            *string
	Irreversible       bool
	IrreversibleReason *string
	Batch              *BatchBody
}

type BatchBody struct {
	BatchSize    int
	CursorColumn string
	BatchTable   string
	Template     string
	MinId        *int64
	MaxId        *int64
}

// LoadMigration fetches one migration scoped to its connection.
func LoadMigration(ctx context.Context, client *ent.Client, connectionID, migrationID int) (*ent.Migration, error) {
	entry, err := client.Migration.Query().
		Where(migration.ID(migrationID), migration.ConnectionID(connectionID)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	return entry, err
}

// CreateMigration inserts a draft migration with the next version number
// for the connection, inside a transaction so concurrent authors stay linear.
func CreateMigration(ctx context.Context, client *ent.Client, userID, connectionID int, body *CreateBody) (*ent.Migration, error) {
	if err := ValidateWrite(body); err != nil {
		return nil, err
	}
	tx, err := client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	entry, err := tx.Migration.Query().
		Where(migration.ConnectionID(connectionID)).
		Order(ent.Desc(migration.FieldVersion)).
		First(ctx)
	if err != nil && !ent.IsNotFound(err) {
		_ = tx.Rollback()
		return nil, err
	}
	version := 1
	if entry != nil {
		version = entry.Version + 1
	}
	create := tx.Migration.Create().
		SetConnectionID(connectionID).
		SetVersion(version).
		SetSQLUp(body.SqlUp).
		SetSQLDown(deref(body.SqlDown)).
		SetIrreversible(body.Irreversible).
		SetIrreversibleReason(deref(body.IrreversibleReason)).
		SetAuthorID(userID).
		SetStatus(migration.StatusDraft)
	if batch := body.Batch; batch != nil {
		create.SetBatchSize(batch.BatchSize).
			SetBatchTable(batch.BatchTable).
			SetCursorColumn(batch.CursorColumn).
			SetTemplate(batch.Template).
			SetNillableMinID(batch.MinId).
			SetNillableMaxID(batch.MaxId)
	}
	created, err := create.Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return created, nil
}

// UpdateMigration edits a migration that has never been submitted.
func UpdateMigration(ctx context.Context, client *ent.Client, userID, connectionID, migrationID int, body *CreateBody) (*ent.Migration, error) {
	if err := ValidateWrite(body); err != nil {
		return nil, err
	}
	entry, err := LoadMigration(ctx, client, connectionID, migrationID)
	if err != nil {
		return nil, err
	}
	if entry.Status != migration.StatusDraft {
		return nil, fmt.Errorf("%w: only draft migrations can be edited", ErrConflict)
	}
	update := entry.Update().
		SetSQLUp(body.SqlUp).
		SetSQLDown(deref(body.SqlDown)).
		SetIrreversible(body.Irreversible).
		SetIrreversibleReason(deref(body.IrreversibleReason))
	if batch := body.Batch; batch != nil {
		update.SetBatchSize(batch.BatchSize).
			SetBatchTable(batch.BatchTable).
			SetCursorColumn(batch.CursorColumn).
			SetTemplate(batch.Template).
			SetNillableMinID(batch.MinId).
			SetNillableMaxID(batch.MaxId)
	} else {
		update.ClearBatchSize().ClearBatchTable().ClearCursorColumn().ClearTemplate().ClearMinID().ClearMaxID()
	}
	return update.Save(ctx)
}

// DeleteMigration hard-deletes a draft migration; nothing else may exist yet.
func DeleteMigration(ctx context.Context, client *ent.Client, userID, connectionID, migrationID int) error {
	entry, err := LoadMigration(ctx, client, connectionID, migrationID)
	if err != nil {
		return err
	}
	if entry.Status != migration.StatusDraft {
		return fmt.Errorf("%w: only draft migrations can be deleted", ErrConflict)
	}
	return client.Migration.DeleteOneID(entry.ID).Exec(ctx)
}

// ExecutionCandidate is one pending_review execution plus its approval rows,
// created by submit/rollback/forward/rebatch flows.
type ExecutionCandidate struct {
	Kind                migrationexecution.Kind
	RevertedExecutionID *int
	SQLDownExecuted     string
}

// CreateExecutionWithApprovals runs inside a tx: one execution plus approval
// rows for every group with any grant on the connection, migration flips to
// pending_review. Returns a sentinel when some group has no leader.
func CreateExecutionWithApprovals(ctx context.Context, tx *ent.Tx, userID, connectionID, migrationID int, cand ExecutionCandidate) error {
	if _, err := tx.MigrationExecution.Create().
		SetMigrationID(migrationID).
		SetConnectionID(connectionID).
		SetKind(cand.Kind).
		SetStatus(migrationexecution.StatusPendingReview).
		SetActorID(userID).
		SetNillableRevertedExecutionID(cand.RevertedExecutionID).
		SetSQLDownExecuted(cand.SQLDownExecuted).
		Save(ctx); err != nil {
		return err
	}
	grants, err := tx.DatabaseGrant.Query().
		Where(databasegrant.ConnectionID(connectionID)).
		All(ctx)
	if err != nil {
		return err
	}
	// A new review cycle (rollback/forward/retry) reuses this migration's
	// approval rows: reset them to pending instead of inserting duplicates.
	if _, err := tx.MigrationApproval.Delete().
		Where(migrationapproval.MigrationID(migrationID)).
		Exec(ctx); err != nil {
		return err
	}
	seen := map[int]bool{}
	for _, g := range grants {
		if seen[g.GroupID] {
			continue
		}
		seen[g.GroupID] = true
		leader, err := leaderOf(ctx, tx.Client(), g.GroupID)
		if err != nil {
			return err
		}
		if leader == 0 {
			return ErrNoLeaderGroup
		}
		if _, err := tx.MigrationApproval.Create().
			SetMigrationID(migrationID).
			SetGroupID(g.GroupID).
			SetLeaderID(leader).
			SetStatus(migrationapproval.StatusPending).
			Save(ctx); err != nil {
			return err
		}
	}
	if err := tx.Migration.UpdateOneID(migrationID).SetStatus(migration.StatusPendingReview).Exec(ctx); err != nil {
		return err
	}
	return nil
}

// leaderOf returns the user id of the group's current leader, or 0 when the
// group has none.
func leaderOf(ctx context.Context, client *ent.Client, groupID int) (int, error) {
	memberships, err := client.GroupMembership.Query().
		Where(groupmembership.GroupID(groupID), groupmembership.RoleEQ(groupmembership.RoleLeader)).
		All(ctx)
	if err != nil {
		return 0, err
	}
	if len(memberships) == 0 {
		return 0, nil
	}
	return memberships[0].UserID, nil
}

// Submit moves a draft migration into review, creating its up execution.
func Submit(ctx context.Context, client *ent.Client, userID, connectionID, migrationID int) (*ent.Migration, error) {
	entry, err := LoadMigration(ctx, client, connectionID, migrationID)
	if err != nil {
		return nil, err
	}
	if entry.Status != migration.StatusDraft {
		return nil, fmt.Errorf("%w: only draft migrations can be submitted", ErrConflict)
	}
	tx, err := client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	err = CreateExecutionWithApprovals(ctx, tx, userID, connectionID, migrationID, ExecutionCandidate{Kind: migrationexecution.KindUp})
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	updated, err := tx.Migration.Query().Where(migration.ID(migrationID)).Only(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return updated, nil
}

// RequestRollback creates a rollback execution for the migration's active
// execution when the migration is the connection's latest still-applied one.
func RequestRollback(ctx context.Context, client *ent.Client, userID, connectionID, migrationID int) (*ent.Migration, error) {
	entry, err := LoadMigration(ctx, client, connectionID, migrationID)
	if err != nil {
		return nil, err
	}
	if entry.Irreversible || strings.TrimSpace(entry.SQLDown) == "" {
		return nil, fmt.Errorf("%w: migration is not reversible", ErrConflict)
	}
	if err := latestActive(ctx, client, connectionID, migrationID); err != nil {
		return nil, err
	}
	active, err := activeExecution(ctx, client, migrationID)
	if err != nil {
		return nil, err
	}
	reverted := active.ID
	tx, err := client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	err = CreateExecutionWithApprovals(ctx, tx, userID, connectionID, migrationID, ExecutionCandidate{
		Kind:                migrationexecution.KindRollback,
		RevertedExecutionID: &reverted,
	})
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	updated, err := tx.Migration.Query().Where(migration.ID(migrationID)).Only(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return updated, nil
}

// latestActive rejects a rollback when a later migration on the same
// connection is still applied.
func latestActive(ctx context.Context, client *ent.Client, connectionID, migrationID int) error {
	entry, err := LoadMigration(ctx, client, connectionID, migrationID)
	if err != nil {
		return err
	}
	later, err := client.Migration.Query().
		Where(
			migration.ConnectionID(connectionID),
			migration.VersionGT(entry.Version),
			migration.StatusIn(
				migration.StatusQueued, migration.StatusRunning,
				migration.StatusSuccess, migration.StatusPartial,
			),
		).
		Exist(ctx)
	if err != nil {
		return err
	}
	if later {
		return fmt.Errorf("%w: migration is not the latest active migration", ErrConflict)
	}
	return nil
}

// activeExecution finds the migration's most recent success up/forward run
// that no later successful rollback has cancelled.
func activeExecution(ctx context.Context, client *ent.Client, migrationID int) (*ent.MigrationExecution, error) {
	executions, err := client.MigrationExecution.Query().
		Where(migrationexecution.MigrationID(migrationID)).
		Order(ent.Asc(migrationexecution.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	reverted := map[int]bool{}
	var active *ent.MigrationExecution
	for _, e := range executions {
		switch {
		case (e.Kind == migrationexecution.KindUp || e.Kind == migrationexecution.KindForward) && e.Status == migrationexecution.StatusSuccess:
			active = e
		case e.Kind == migrationexecution.KindRollback && e.Status == migrationexecution.StatusSuccess && e.RevertedExecutionID != nil:
			reverted[*e.RevertedExecutionID] = true
			active = nil
		}
	}
	if active == nil || reverted[active.ID] {
		return nil, fmt.Errorf("%w: migration has no active execution to roll back", ErrConflict)
	}
	return active, nil
}

// RequestForward re-activates a rolled-back migration, or retries a failed
// or partial one, depending on the migration's last execution.
func RequestForward(ctx context.Context, client *ent.Client, userID, connectionID, migrationID int) (*ent.Migration, error) {
	entry, err := LoadMigration(ctx, client, connectionID, migrationID)
	if err != nil {
		return nil, err
	}
	kind, err := forwardKind(ctx, client, entry)
	if err != nil {
		return nil, err
	}
	tx, err := client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	err = CreateExecutionWithApprovals(ctx, tx, userID, connectionID, migrationID, ExecutionCandidate{Kind: kind})
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	updated, err := tx.Migration.Query().Where(migration.ID(migrationID)).Only(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return updated, nil
}

func forwardKind(ctx context.Context, client *ent.Client, entry *ent.Migration) (migrationexecution.Kind, error) {
	last, err := client.MigrationExecution.Query().
		Where(migrationexecution.MigrationID(entry.ID)).
		Order(ent.Desc(migrationexecution.FieldCreatedAt)).
		First(ctx)
	if ent.IsNotFound(err) {
		return "", fmt.Errorf("%w: migration has no executions to forward", ErrConflict)
	}
	if err != nil {
		return "", err
	}
	switch {
	case entry.Status == migration.StatusFailed || entry.Status == migration.StatusPartial:
		if last.Status != migrationexecution.StatusFailed && last.Status != migrationexecution.StatusPartial {
			return "", fmt.Errorf("%w: forward is only valid after a rollback", ErrConflict)
		}
		return migrationexecution.KindRetry, nil
	case last.Kind == migrationexecution.KindRollback && last.Status == migrationexecution.StatusSuccess:
		return migrationexecution.KindForward, nil
	default:
		return "", fmt.Errorf("%w: forward is only valid after a successful rollback", ErrConflict)
	}
}

// RequestBatchRevert queues compensation for one partial batch execution.
func RequestBatchRevert(ctx context.Context, client *ent.Client, userID, connectionID, migrationID int, executionID int, sqlDown string, template *string) (*ent.Migration, error) {
	if strings.TrimSpace(sqlDown) == "" && (template == nil || strings.TrimSpace(*template) == "") {
		return nil, fmt.Errorf("%w: sql_down or template is required", ErrValidation)
	}
	entry, err := LoadMigration(ctx, client, connectionID, migrationID)
	if err != nil {
		return nil, err
	}
	if _, err := client.MigrationExecution.Query().
		Where(
			migrationexecution.ID(executionID),
			migrationexecution.MigrationID(entry.ID),
			migrationexecution.KindEQ(migrationexecution.KindUp),
			migrationexecution.StatusEQ(migrationexecution.StatusPartial),
		).
		Only(ctx); ent.IsNotFound(err) {
		return nil, fmt.Errorf("%w: execution is not a partial batch run of this migration", ErrConflict)
	} else if err != nil {
		return nil, err
	}
	compensation := sqlDown
	if template != nil && strings.TrimSpace(*template) != "" {
		compensation = *template
	}
	tx, err := client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	err = CreateExecutionWithApprovals(ctx, tx, userID, connectionID, migrationID, ExecutionCandidate{
		Kind:            migrationexecution.KindBatchRevert,
		SQLDownExecuted: compensation,
	})
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	updated, err := tx.Migration.Query().Where(migration.ID(migrationID)).Only(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return updated, nil
}

// Approve records the caller's group decision; when every approval is in,
// the migration and execution move to queued and a job row is created.
func Approve(ctx context.Context, client *ent.Client, userID, approvalID int) (*ent.Migration, *ent.MigrationExecution, *ent.MigrationJob, error) {
	approval, err := client.MigrationApproval.Query().
		Where(migrationapproval.ID(approvalID)).
		WithGroup().
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, nil, err
	}
	leader, err := leaderOf(ctx, client, approval.GroupID)
	if err != nil {
		return nil, nil, nil, err
	}
	if leader != userID {
		return nil, nil, nil, ErrNotLeader
	}
	if approval.Status != migrationapproval.StatusPending {
		return nil, nil, nil, fmt.Errorf("%w: approval already decided", ErrConflict)
	}
	tx, err := client.Tx(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	now := time.Now().UTC()
	if err := tx.MigrationApproval.UpdateOneID(approvalID).
		SetStatus(migrationapproval.StatusApproved).
		SetDecidedByID(userID).
		SetDecidedAt(now).
		Exec(ctx); err != nil {
		_ = tx.Rollback()
		return nil, nil, nil, err
	}
	exec, err := pendingExecution(ctx, tx.Client(), approval.MigrationID)
	if err != nil {
		_ = tx.Rollback()
		return nil, nil, nil, err
	}
	remaining, err := tx.MigrationApproval.Query().
		Where(migrationapproval.MigrationID(approval.MigrationID), migrationapproval.StatusEQ(migrationapproval.StatusPending)).
		Count(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, nil, nil, err
	}
	entry, err := tx.Migration.Query().Where(migration.ID(approval.MigrationID)).Only(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, nil, nil, err
	}
	var job *ent.MigrationJob
	if remaining == 0 {
		if err := tx.Migration.UpdateOneID(entry.ID).SetStatus(migration.StatusQueued).Exec(ctx); err != nil {
			_ = tx.Rollback()
			return nil, nil, nil, err
		}
		if err := tx.MigrationExecution.UpdateOneID(exec.ID).SetStatus(migrationexecution.StatusQueued).Exec(ctx); err != nil {
			_ = tx.Rollback()
			return nil, nil, nil, err
		}
		job, err = tx.MigrationJob.Create().
			SetExecutionID(exec.ID).
			SetConnectionID(entry.ConnectionID).
			SetStatus(migrationjob.StatusQueued).
			SetTraceparent(tracing.W3CTraceparent(ctx)).
			Save(ctx)
		if err != nil {
			_ = tx.Rollback()
			return nil, nil, nil, err
		}
	} else {
		if err := tx.Migration.UpdateOneID(entry.ID).SetStatus(migration.StatusPendingReview).Exec(ctx); err != nil {
			_ = tx.Rollback()
			return nil, nil, nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, nil, err
	}
	// Re-read so callers see the post-approval status (queued or pending_review).
	entry, err = client.Migration.Query().Where(migration.ID(entry.ID)).Only(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	return entry, exec, job, nil
}

// Reject turns one approval down: the migration goes back to draft and the
// never-run execution is deleted. Approval rows remain for audit.
func Reject(ctx context.Context, client *ent.Client, userID, approvalID int, comment *string) (*ent.Migration, error) {
	approval, err := client.MigrationApproval.Query().
		Where(migrationapproval.ID(approvalID)).
		WithGroup().
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	leader, err := leaderOf(ctx, client, approval.GroupID)
	if err != nil {
		return nil, err
	}
	if leader != userID {
		return nil, ErrNotLeader
	}
	if approval.Status != migrationapproval.StatusPending {
		return nil, fmt.Errorf("%w: approval already decided", ErrConflict)
	}
	tx, err := client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if err := tx.MigrationApproval.UpdateOneID(approvalID).
		SetStatus(migrationapproval.StatusRejected).
		SetDecidedByID(userID).
		SetDecidedAt(now).
		SetComment(deref(comment)).
		Exec(ctx); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	exec, err := pendingExecution(ctx, tx.Client(), approval.MigrationID)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.MigrationExecution.DeleteOneID(exec.ID).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Migration.UpdateOneID(approval.MigrationID).SetStatus(migration.StatusDraft).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	updated, err := tx.Migration.Query().Where(migration.ID(approval.MigrationID)).Only(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return updated, nil
}

// pendingExecution loads the migration's single pending_review execution.
func pendingExecution(ctx context.Context, client *ent.Client, migrationID int) (*ent.MigrationExecution, error) {
	return client.MigrationExecution.Query().
		Where(
			migrationexecution.MigrationID(migrationID),
			migrationexecution.StatusEQ(migrationexecution.StatusPendingReview),
		).
		Order(ent.Desc(migrationexecution.FieldCreatedAt)).
		First(ctx)
}

// MyApprovals lists approvals the caller can act on or has decided: current
// group leader, or decided_by them, with migration and author for display.
func MyApprovals(ctx context.Context, client *ent.Client, userID int, status *migrationapproval.Status) ([]*ent.MigrationApproval, error) {
	query := client.MigrationApproval.Query().
		WithGroup().
		WithLeader().
		WithMigration(func(mq *ent.MigrationQuery) {
			mq.WithAuthor()
		})
	if status != nil {
		query = query.Where(migrationapproval.StatusEQ(*status))
	}
	rows, err := query.All(ctx)
	if err != nil {
		return nil, err
	}
	leaderOf := map[int]bool{}
	memberships, err := client.GroupMembership.Query().
		Where(groupmembership.UserID(userID), groupmembership.RoleEQ(groupmembership.RoleLeader)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range memberships {
		leaderOf[m.GroupID] = true
	}
	result := make([]*ent.MigrationApproval, 0, len(rows))
	for _, a := range rows {
		if leaderOf[a.GroupID] || (a.DecidedByID != nil && *a.DecidedByID == userID) {
			result = append(result, a)
		}
	}
	return result, nil
}

// GetJob loads one queue row with its execution.
func GetJob(ctx context.Context, client *ent.Client, jobID int) (*ent.MigrationJob, error) {
	return client.MigrationJob.Query().
		Where(migrationjob.ID(jobID)).
		WithExecution(func(q *ent.MigrationExecutionQuery) {
			q.WithMigration()
		}).
		Only(ctx)
}

// CancelJob applies the cancellation state machine directly: queued jobs
// cancel in place, running jobs are flagged cancelling so the worker stops
// at the next batch boundary. Terminal jobs are a conflict.
func CancelJob(ctx context.Context, client *ent.Client, jobID int) (*ent.MigrationJob, error) {
	entry, err := client.MigrationJob.Query().Where(migrationjob.ID(jobID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	switch entry.Status {
	case migrationjob.StatusQueued:
		updated, err := entry.Update().
			Where(migrationjob.StatusEQ(migrationjob.StatusQueued)).
			SetStatus(migrationjob.StatusCancelled).
			SetFinishedAt(time.Now().UTC()).
			Save(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: job already decided", ErrConflict)
		}
		if _, err := client.MigrationExecution.UpdateOneID(entry.ExecutionID).
			Where(migrationexecution.StatusEQ(migrationexecution.StatusQueued)).
			SetStatus(migrationexecution.StatusCancelled).
			SetFinishedAt(time.Now().UTC()).
			Save(ctx); err != nil && !ent.IsNotFound(err) {
			return nil, err
		}
		return updated, nil
	case migrationjob.StatusRunning:
		updated, err := entry.Update().
			Where(migrationjob.StatusEQ(migrationjob.StatusRunning)).
			SetStatus(migrationjob.StatusCancelling).
			Save(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: job already decided", ErrConflict)
		}
		return updated, nil
	default:
		return nil, fmt.Errorf("%w: job already finished", ErrConflict)
	}
}

// MigrationWithHistory loads a migration with executions and approvals
// eager-loaded for the list/detail mappers.
func MigrationWithHistory(ctx context.Context, client *ent.Client, entry *ent.Migration) (*ent.Migration, error) {
	return client.Migration.Query().
		Where(migration.ID(entry.ID)).
		WithExecutions(func(q *ent.MigrationExecutionQuery) {
			q.WithActor().Order(ent.Desc(migrationexecution.FieldCreatedAt))
		}).
		WithApprovals().
		Only(ctx)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
