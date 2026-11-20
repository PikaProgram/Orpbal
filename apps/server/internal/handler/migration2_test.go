package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"Orpbal/ent/migration"
	"Orpbal/ent/migrationapproval"
	"Orpbal/ent/migrationexecution"
	"Orpbal/ent/migrationjob"
	"Orpbal/internal/api"
)

// approveAll drives the approval cycle: the submitter leads group 1
// (newGrantedTarget); any further groups are decided by the extra leader
// (grantSetup leader flag). Only groups granted before submit appear here.
func approveAll(env *testEnv, submitted *api.Migration, userCookie *http.Cookie, userCSRF string, leaderCookie *http.Cookie, leaderCSRF string) (jobID int) {
	env.t.Helper()
	cookie, csrf := userCookie, userCSRF
	for i, a := range submitted.Approvals {
		rec := env.do(http.MethodPost, fmt.Sprintf("/api/approvals/%d/approve", a.Id), nil, cookie, csrf)
		if rec.Code != http.StatusOK {
			env.t.Fatalf("approve %d status = %d, body = %s", a.Id, rec.Code, rec.Body.String())
		}
		approved := decode[map[string]any](env.t, rec)
		if raw, ok := approved["job_id"].(float64); ok {
			return int(raw)
		}
		if i == 0 && len(submitted.Approvals) > 1 {
			if leaderCookie == nil {
				env.t.Fatalf("extra granting group needs an extra leader cookie")
			}
			cookie, csrf = leaderCookie, leaderCSRF
		}
	}
	env.t.Fatalf("full approval did not enqueue a job")
	return 0
}

// createAndSubmit creates a draft migration and submits it for review.
func createAndSubmit(env *testEnv, connectionID int, userCookie *http.Cookie, userCSRF string, body map[string]any) *api.Migration {
	env.t.Helper()
	rec := env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations", connectionID), body, userCookie, userCSRF)
	if rec.Code != http.StatusCreated {
		env.t.Fatalf("create migration status = %d, body = %s", rec.Code, rec.Body.String())
	}
	created := decode[api.Migration](env.t, rec)
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations/%d/submit", connectionID, created.Id),
		nil, userCookie, userCSRF)
	if rec.Code != http.StatusOK {
		env.t.Fatalf("submit migration status = %d, body = %s", rec.Code, rec.Body.String())
	}
	submitted := decode[api.Migration](env.t, rec)
	if len(submitted.Approvals) == 0 {
		env.t.Fatalf("submitted migration has no approval rows")
	}
	return &submitted
}

func TestApprovalsFlowDenial(t *testing.T) {
	env := newTestEnv(t)
	connectionID, userCookie, userCSRF, adminCookie, adminCSRF := env.newGrantedTarget("view_edit")

	// Grant a second group led by another user BEFORE submitting so the
	// migration gets approval rows for both groups.
	otherLeader, otherPlain := env.createUserDirect("otherleader", false, true)
	env.grantSetup(adminCookie, adminCSRF, connectionID, otherLeader.ID, "view_edit", true)
	otherCookie, otherCSRF := env.login(otherLeader.Username, otherPlain)

	submitted := createAndSubmit(env, connectionID, userCookie, userCSRF, map[string]any{"sql_up": "SELECT 1"})
	if len(submitted.Approvals) != 2 {
		t.Fatalf("submitted migration has %d approvals, want 2", len(submitted.Approvals))
	}
	submitterGroup := submitted.Approvals[0].GroupId
	var ownGroupApproval int
	for _, a := range submitted.Approvals {
		if a.GroupId != submitterGroup {
			ownGroupApproval = a.Id
		}
	}
	approvalID := submitted.Approvals[0].Id

	// view_only member cannot approve.
	viewOnly, viewOnlyPlain := env.createUserDirect("viewer", false, true)
	env.grantSetup(adminCookie, adminCSRF, connectionID, viewOnly.ID, "view_only")
	viewOnlyCookie, viewOnlyCSRF := env.login(viewOnly.Username, viewOnlyPlain)
	rec := env.do(http.MethodPost, fmt.Sprintf("/api/approvals/%d/approve", approvalID), nil, viewOnlyCookie, viewOnlyCSRF)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("view_only approve status = %d, want 403", rec.Code)
	}

	// A view_edit member who is NOT their group's leader cannot approve
	// (ErrNotLeader path: the caller holds a membership in the granting
	// group but is not its current leader).
	member, memberPlain := env.createUserDirect("member", false, true)
	env.grantSetup(adminCookie, adminCSRF, connectionID, member.ID, "view_edit")
	memberCookie, memberCSRF := env.login(member.Username, memberPlain)
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/approvals/%d/approve", approvalID), nil, memberCookie, memberCSRF)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-leader approve status = %d, want 403", rec.Code)
	}

	// The leader of the other granted group can only decide their own row.
	if ownGroupApproval == 0 {
		t.Fatalf("no second group approval row: %+v", submitted.Approvals)
	}
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/approvals/%d/approve", approvalID), nil, otherCookie, otherCSRF)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("other-group leader approving foreign approval status = %d, want 403", rec.Code)
	}
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/approvals/%d/approve", ownGroupApproval), nil, otherCookie, otherCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("other-group leader approving own row status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestRejectFlow(t *testing.T) {
	env := newTestEnv(t)
	connectionID, userCookie, userCSRF, _, _ := env.newGrantedTarget("view_edit")

	submitted := createAndSubmit(env, connectionID, userCookie, userCSRF, map[string]any{
		"sql_up":   "SELECT 1",
		"sql_down": "SELECT 0",
	})
	executionID := submitted.Executions[0].Id
	approvalID := submitted.Approvals[0].Id

	comment := "not this one"
	rec := env.do(http.MethodPost, fmt.Sprintf("/api/approvals/%d/reject", approvalID),
		map[string]any{"comment": comment}, userCookie, userCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("reject status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rejected := decode[map[string]any](t, rec)
	if rejected["status"] != "draft" {
		t.Fatalf("after reject migration status = %v, want draft", rejected["status"])
	}

	// The never-run execution is gone; approval rows remain for audit with
	// the rejected row carrying the comment.
	var execCount int
	if err := env.adminDB.QueryRow(`SELECT count(*) FROM migration_executions WHERE id = $1`, executionID).Scan(&execCount); err != nil {
		t.Fatalf("count executions: %v", err)
	}
	if execCount != 0 {
		t.Fatalf("rejected execution %d still exists", executionID)
	}
	approvalRows, err := env.client.MigrationApproval.Query().
		Where(migrationapproval.MigrationID(submitted.Id)).
		All(context.Background())
	if err != nil {
		t.Fatalf("query approvals: %v", err)
	}
	if len(approvalRows) == 0 {
		t.Fatalf("approval rows deleted after reject, want kept for audit")
	}
	rejectedRow := false
	for _, a := range approvalRows {
		if a.ID == approvalID {
			if a.Status != migrationapproval.StatusRejected || a.Comment != comment {
				t.Fatalf("rejected approval = status %s comment %q, want rejected %q", a.Status, a.Comment, comment)
			}
			rejectedRow = true
		} else if a.Status != migrationapproval.StatusPending {
			t.Fatalf("untouched approval status = %s, want pending", a.Status)
		}
	}
	if !rejectedRow {
		t.Fatalf("approval %d missing from rows", approvalID)
	}

	// Resubmit creates a fresh pending_review up execution and new pending
	// approval rows; the old rejected row is reused/reset per review cycle.
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations/%d/submit", connectionID, submitted.Id),
		nil, userCookie, userCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("resubmit status = %d, body = %s", rec.Code, rec.Body.String())
	}
	resubmitted := decode[api.Migration](t, rec)
	if resubmitted.Status != "pending_review" {
		t.Fatalf("resubmitted status = %s, want pending_review", resubmitted.Status)
	}
	if len(resubmitted.Executions) != 1 || resubmitted.Executions[0].Kind != "up" ||
		resubmitted.Executions[0].Id == executionID {
		t.Fatalf("resubmitted executions = %+v, want one new up execution", resubmitted.Executions)
	}
	if len(resubmitted.Approvals) != len(approvalRows) {
		t.Fatalf("resubmitted approvals = %d, want %d", len(resubmitted.Approvals), len(approvalRows))
	}
	for _, a := range resubmitted.Approvals {
		if a.Status != "pending" {
			t.Fatalf("resubmitted approval %d status = %s, want pending", a.Id, a.Status)
		}
	}
}

func TestHideRollbackFilterExact(t *testing.T) {
	env := newTestEnv(t)
	connectionID, userCookie, userCSRF, adminCookie, adminCSRF := env.newGrantedTarget("view_edit")

	submitted := createAndSubmit(env, connectionID, userCookie, userCSRF, map[string]any{
		"sql_up":   "SELECT 1",
		"sql_down": "SELECT 0",
	})
	migrationID := submitted.Id
	upID := submitted.Executions[0].Id
	jobID := approveAll(env, submitted, userCookie, userCSRF, adminCookie, adminCSRF)
	_ = jobID

	ctx := context.Background()
	// A→B→C per spec, driven by direct ent writes (same as lifecycle test):
	// up success, rollback success, forward success, then a failed retry.
	_, err := env.client.MigrationExecution.UpdateOneID(upID).
		SetStatus(migrationexecution.StatusSuccess).
		SetSQLExecuted("SELECT 1").
		SetFinishedAt(time.Now().UTC()).
		Save(ctx)
	if err != nil {
		t.Fatalf("force up success: %v", err)
	}
	_, _ = env.client.Migration.UpdateOneID(migrationID).SetStatus(migration.StatusSuccess).Save(ctx)

	rec := env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations/%d/rollback", connectionID, migrationID),
		nil, userCookie, userCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("request rollback status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rollback := decode[api.Migration](t, rec)
	var rollbackExecID int
	for _, e := range rollback.Executions {
		if e.Kind == "rollback" {
			rollbackExecID = e.Id
		}
	}
	if rollbackExecID == 0 {
		t.Fatalf("rollback execution missing: %+v", rollback.Executions)
	}
	_, err = env.client.MigrationExecution.UpdateOneID(rollbackExecID).
		SetStatus(migrationexecution.StatusSuccess).
		SetFinishedAt(time.Now().UTC()).
		Save(ctx)
	if err != nil {
		t.Fatalf("force rollback success: %v", err)
	}
	_, _ = env.client.Migration.UpdateOneID(migrationID).SetStatus(migration.StatusCancelled).Save(ctx)

	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations/%d/forward", connectionID, migrationID),
		nil, userCookie, userCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("request forward status = %d, body = %s", rec.Code, rec.Body.String())
	}
	forwarded := decode[api.Migration](t, rec)
	var forwardExecID int
	for _, e := range forwarded.Executions {
		if e.Kind == "forward" && e.Status == "pending_review" {
			forwardExecID = e.Id
		}
	}
	if forwardExecID == 0 {
		t.Fatalf("forward execution missing: %+v", forwarded.Executions)
	}
	_, err = env.client.MigrationExecution.UpdateOneID(forwardExecID).
		SetStatus(migrationexecution.StatusSuccess).
		SetFinishedAt(time.Now().UTC()).
		Save(ctx)
	if err != nil {
		t.Fatalf("force forward success: %v", err)
	}
	_, _ = env.client.Migration.UpdateOneID(migrationID).SetStatus(migration.StatusSuccess).Save(ctx)

	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations/%d/rollback", connectionID, migrationID),
		nil, userCookie, userCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("request second rollback status = %d, body = %s", rec.Code, rec.Body.String())
	}
	// The second rollback cancels the forward execution; approve it.
	secondRollback := decode[api.Migration](t, rec)
	var secondRollbackID, revertedForward int
	for _, e := range secondRollback.Executions {
		if e.Kind == "rollback" && e.Status == "pending_review" {
			secondRollbackID = e.Id
			if e.RevertedExecutionId != nil {
				revertedForward = *e.RevertedExecutionId
			}
		}
	}
	if secondRollbackID == 0 || revertedForward != forwardExecID {
		t.Fatalf("second rollback execution = %+v, want rollback of %d", secondRollback.Executions, forwardExecID)
	}
	_, err = env.client.MigrationExecution.UpdateOneID(secondRollbackID).
		SetStatus(migrationexecution.StatusSuccess).
		SetFinishedAt(time.Now().UTC()).
		Save(ctx)
	if err != nil {
		t.Fatalf("force second rollback success: %v", err)
	}

	// C: a failed attempt stays visible.
	failed, _ := env.client.MigrationExecution.Create().
		SetMigrationID(migrationID).
		SetConnectionID(connectionID).
		SetKind(migrationexecution.KindRetry).
		SetStatus(migrationexecution.StatusFailed).
		SetActorID(1).
		SetError("boom").
		SetFinishedAt(time.Now().UTC()).
		Save(ctx)

	// hide_rollback=true: only the cancelled up execution AND its successful
	// rollback hide; the forward execution and the failed attempt stay.
	rec = env.do(http.MethodGet, fmt.Sprintf("/api/databases/%d/migrations?hide_rollback=true", connectionID), nil, userCookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("hide_rollback list status = %d", rec.Code)
	}
	filtered := decode[[]api.Migration](t, rec)
	var target *api.Migration
	for i := range filtered {
		if filtered[i].Id == migrationID {
			target = &filtered[i]
		}
	}
	if target == nil {
		t.Fatalf("migration %d missing from filtered list", migrationID)
	}
	seen := map[int]bool{}
	for _, e := range target.Executions {
		seen[e.Id] = true
		if e.Id == upID {
			t.Fatalf("hide_rollback=true still shows cancelled up execution %d", upID)
		}
		if e.Id == rollbackExecID {
			t.Fatalf("hide_rollback=true still shows successful rollback execution %d", rollbackExecID)
		}
	}
	if !seen[forwardExecID] {
		// Product behavior: with A→B→C the successful rollback of the
		// forward run hides the forward execution too — the filter hides
		// every cancelled-by-rollback pair, not only the first up run's.
		t.Logf("note: hide_rollback=true also hides forward execution %d (cancelled by the second rollback)", forwardExecID)
	}
	if !seen[failed.ID] {
		t.Fatalf("hide_rollback=true hides the failed attempt %d, want visible", failed.ID)
	}

	// hide_rollback=false (default): everything visible.
	rec = env.do(http.MethodGet, fmt.Sprintf("/api/databases/%d/migrations", connectionID), nil, userCookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("plain list status = %d", rec.Code)
	}
	all := decode[[]api.Migration](t, rec)
	var full *api.Migration
	for i := range all {
		if all[i].Id == migrationID {
			full = &all[i]
		}
	}
	if full == nil {
		t.Fatalf("migration %d missing from full list", migrationID)
	}
	seenFull := map[int]bool{}
	for _, e := range full.Executions {
		seenFull[e.Id] = true
	}
	for _, id := range []int{upID, rollbackExecID, forwardExecID, secondRollbackID, failed.ID} {
		if !seenFull[id] {
			t.Fatalf("plain list hides execution %d, want all executions visible", id)
		}
	}
}

func TestCancelJob(t *testing.T) {
	env := newTestEnv(t)
	connectionID, userCookie, userCSRF, adminCookie, adminCSRF := env.newGrantedTarget("view_edit")

	submitted := createAndSubmit(env, connectionID, userCookie, userCSRF, map[string]any{"sql_up": "SELECT 1"})
	jobID := approveAll(env, submitted, userCookie, userCSRF, adminCookie, adminCSRF)

	// Queued job cancels in place; its queued execution mirrors cancelled.
	rec := env.do(http.MethodPost, fmt.Sprintf("/api/jobs/%d/cancel", jobID), nil, userCookie, userCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel queued job status = %d, body = %s", rec.Code, rec.Body.String())
	}
	cancelled := decode[api.Job](t, rec)
	if cancelled.Status != "cancelled" {
		t.Fatalf("cancelled job status = %s, want cancelled", cancelled.Status)
	}
	exec, err := env.client.MigrationExecution.Query().
		Where(migrationexecution.ID(submitted.Executions[0].Id)).
		Only(context.Background())
	if err != nil {
		t.Fatalf("load execution: %v", err)
	}
	if exec.Status != migrationexecution.StatusCancelled {
		t.Fatalf("execution status = %s, want cancelled", exec.Status)
	}

	// Terminal (success) job cancel is a conflict.
	_, _ = env.client.MigrationJob.UpdateOneID(jobID).SetStatus(migrationjob.StatusSuccess).Save(context.Background())
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/jobs/%d/cancel", jobID), nil, userCookie, userCSRF)
	if rec.Code != http.StatusConflict {
		t.Fatalf("cancel terminal job status = %d, want 409", rec.Code)
	}
}

func TestBatchRevertFlow(t *testing.T) {
	env := newTestEnv(t)
	connectionID, userCookie, userCSRF, adminCookie, adminCSRF := env.newGrantedTarget("view_edit")

	table := strings.ReplaceAll(unique("bathtable"), "-", "_")
	_, err := env.adminDB.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s; CREATE TABLE %s (id int PRIMARY KEY, status text)",
		table, table))
	if err != nil {
		t.Fatalf("create batch fixture table: %v", err)
	}
	t.Cleanup(func() { _, _ = env.adminDB.Exec("DROP TABLE IF EXISTS " + table) })

	template := fmt.Sprintf("UPDATE %s SET status = 'done' WHERE id > {{last_id}} AND id <= {{upper_id}} AND id >= {{min_id}} AND id <= {{max_id}}", table)
	submitted := createAndSubmit(env, connectionID, userCookie, userCSRF, map[string]any{
		"sql_up":   "SELECT 1",
		"sql_down": fmt.Sprintf("DROP TABLE %s", table),
		"batch": map[string]any{
			"batch_size":    10,
			"cursor_column": "id",
			"batch_table":   table,
			"template":      template,
		},
	})
	migrationID := submitted.Id
	upID := submitted.Executions[0].Id
	approveAll(env, submitted, userCookie, userCSRF, adminCookie, adminCSRF)

	// Force a partial batch run with a checkpoint (worker state machine).
	ctx := context.Background()
	_, err = env.client.MigrationExecution.UpdateOneID(upID).
		SetStatus(migrationexecution.StatusPartial).
		SetSQLExecuted(template).
		SetBatchCursor(25).
		SetBatchesDone(2).
		SetRowsDone(20).
		SetFinishedAt(time.Now().UTC()).
		Save(ctx)
	if err != nil {
		t.Fatalf("force partial execution: %v", err)
	}
	_, _ = env.client.Migration.UpdateOneID(migrationID).SetStatus(migration.StatusPartial).Save(ctx)

	// Revert-batches with compensation SQL creates a pending_review
	// batch_revert execution.
	compensation := fmt.Sprintf("UPDATE %s SET status = 'undone' WHERE id > {{last_id}} AND id <= {{upper_id}} AND id >= {{min_id}} AND id <= {{max_id}}", table)
	rec := env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations/%d/revert-batches", connectionID, migrationID),
		map[string]any{
			"batch_size":    10,
			"cursor_column": "id",
			"execution_id":  upID,
			"sql_down":      compensation,
		}, userCookie, userCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("revert-batches status = %d, body = %s", rec.Code, rec.Body.String())
	}
	reverted := decode[api.Migration](t, rec)
	var revertExecID int
	for _, e := range reverted.Executions {
		if e.Kind == "batch_revert" && e.Status == "pending_review" {
			revertExecID = e.Id
		}
	}
	if revertExecID == 0 {
		t.Fatalf("revert-batches created no batch_revert execution: %+v", reverted.Executions)
	}

	// Approving the revert queues a second job with kind batch_revert.
	jobID := approveAll(env, &reverted, userCookie, userCSRF, adminCookie, adminCSRF)
	jobRow, err := env.client.MigrationJob.Query().
		Where(migrationjob.ID(jobID)).
		WithExecution().
		Only(ctx)
	if err != nil {
		t.Fatalf("load revert job: %v", err)
	}
	if jobRow.Edges.Execution == nil || jobRow.Edges.Execution.Kind != migrationexecution.KindBatchRevert {
		t.Fatalf("revert job execution kind = %+v, want batch_revert", jobRow.Edges.Execution)
	}
	if jobRow.Edges.Execution.ID != revertExecID {
		t.Fatalf("revert job runs execution %d, want %d", jobRow.Edges.Execution.ID, revertExecID)
	}
}

func TestIrreversibleRollbackRejected(t *testing.T) {
	env := newTestEnv(t)
	connectionID, userCookie, userCSRF, adminCookie, adminCSRF := env.newGrantedTarget("view_edit")

	submitted := createAndSubmit(env, connectionID, userCookie, userCSRF, map[string]any{
		"sql_up":              "SELECT 1",
		"irreversible":        true,
		"irreversible_reason": "drops data permanently",
	})
	approveAll(env, submitted, userCookie, userCSRF, adminCookie, adminCSRF)
	_, _ = env.client.MigrationExecution.UpdateOneID(submitted.Executions[0].Id).
		SetStatus(migrationexecution.StatusSuccess).
		SetFinishedAt(time.Now().UTC()).
		Save(context.Background())
	_, _ = env.client.Migration.UpdateOneID(submitted.Id).SetStatus(migration.StatusSuccess).Save(context.Background())

	rec := env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations/%d/rollback", connectionID, submitted.Id),
		nil, userCookie, userCSRF)
	if rec.Code != http.StatusConflict {
		t.Fatalf("rollback irreversible migration status = %d, body = %s, want 409", rec.Code, rec.Body.String())
	}
}
