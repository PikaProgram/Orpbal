package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"Orpbal/ent/groupmembership"
	"Orpbal/ent/migrationexecution"
	"Orpbal/ent/migration"
	"Orpbal/ent/migrationjob"
	"Orpbal/internal/api"
)

// grantSetup wires one group with the given access level over one connection
// and makes user a member of it. When leader is true the user becomes the
// group's leader (role is only settable via ent directly, no admin API yet).
func (e *testEnv) grantSetup(adminCookie *http.Cookie, adminCSRF string, connectionID int, userID int, level string, leader ...bool) {
	e.t.Helper()
	rec := e.do(http.MethodPost, "/api/admin/groups", map[string]any{
		"name": unique("group"),
	}, adminCookie, adminCSRF)
	if rec.Code != http.StatusCreated {
		e.t.Fatalf("create group status = %d, body = %s", rec.Code, rec.Body.String())
	}
	group := decode[api.Group](e.t, rec)
	rec = e.do(http.MethodPut, fmt.Sprintf("/api/admin/groups/%d/members/%d", group.Id, userID), nil, adminCookie, adminCSRF)
	if rec.Code != http.StatusNoContent {
		e.t.Fatalf("add member status = %d", rec.Code)
	}
	rec = e.do(http.MethodPut, fmt.Sprintf("/api/admin/groups/%d/grants/%d", group.Id, connectionID), map[string]any{
		"access_level": level,
	}, adminCookie, adminCSRF)
	if rec.Code != http.StatusNoContent {
		e.t.Fatalf("set grant status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(leader) > 0 && leader[0] {
		if _, err := e.client.GroupMembership.Update().
			Where(groupmembership.UserIDEQ(userID), groupmembership.GroupIDEQ(group.Id)).
			SetRole(groupmembership.RoleLeader).
			Save(context.Background()); err != nil {
			e.t.Fatalf("set leader role: %v", err)
		}
	}
}

// newGrantedTarget creates a verified connection plus a view_edit user.
func (e *testEnv) newGrantedTarget(level string) (connectionID int, userCookie *http.Cookie, userCSRF string, adminCookie *http.Cookie, adminCSRF string) {
	e.t.Helper()
	admin, adminPlain := e.createUserDirect("admin", true, true)
	adminCookie, adminCSRF = e.login(admin.Username, adminPlain)

	rec := e.createConnection(adminCookie, adminCSRF, testTargetConnection(e.t))
	if rec.Code != http.StatusCreated {
		e.t.Fatalf("create connection status = %d, body = %s", rec.Code, rec.Body.String())
	}
	connection := e.trackConnection(rec)

	user, userPlain := e.createUserDirect("user", false, true)
	// The user leads their own group: submit requires every granting group to
	// have a leader, and a leaderless group fails review with 409.
	e.grantSetup(adminCookie, adminCSRF, connection.Id, user.ID, level, true)
	userCookie, userCSRF = e.login(user.Username, userPlain)
	return connection.Id, userCookie, userCSRF, adminCookie, adminCSRF
}

func TestViewerSchemaDataAndDenial(t *testing.T) {
	env := newTestEnv(t)
	connectionID, userCookie, userCSRF, adminCookie, adminCSRF := env.newGrantedTarget("view_edit")

	table := unique("migtable")
	table = strings.ReplaceAll(table, "-", "_")
	_, err := env.adminDB.Exec("DROP TABLE IF EXISTS " + table)
	if err != nil {
		t.Fatalf("drop stale fixture table: %v", err)
	}
	_, err = env.adminDB.Exec(fmt.Sprintf(
		"CREATE TABLE %s (id serial PRIMARY KEY, label text NOT NULL)", table))
	if err != nil {
		t.Fatalf("create viewer fixture table: %v", err)
	}
	_, err = env.adminDB.Exec(fmt.Sprintf(
		"INSERT INTO %s (label) VALUES ('one'), ('two'), ('three')", table))
	if err != nil {
		t.Fatalf("seed viewer fixture rows: %v", err)
	}
	t.Cleanup(func() {
		_, _ = env.adminDB.Exec("DROP TABLE IF EXISTS " + table)
	})

	// Schema fetch succeeds for a granted user and lists the fixture table.
	rec := env.do(http.MethodGet, fmt.Sprintf("/api/databases/%d/schema", connectionID), nil, userCookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("schema status = %d, body = %s", rec.Code, rec.Body.String())
	}
	schema := decode[api.DatabaseSchema](t, rec)
	found := false
	for _, entry := range schema.Schemas {
		for _, tableEntry := range entry.Tables {
			if tableEntry.Name == table {
				found = true
				if len(tableEntry.Columns) != 2 {
					t.Fatalf("fixture table has %d columns, want 2", len(tableEntry.Columns))
				}
				if tableEntry.Indexes == nil {
					t.Fatal("schema response has no indexes at all")
				}
				hasPK := false
				for _, constraint := range *tableEntry.Constraints {
					if constraint.Type == "PRIMARY KEY" {
						hasPK = true
					}
				}
				if !hasPK {
					t.Fatal("primary key constraint missing from schema response")
				}
			}
		}
	}
	if !found {
		t.Fatalf("fixture table %q missing from schema response: %+v", table, schema)
	}

	// Empty SQL is rejected.
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/query", connectionID),
		map[string]any{"sql": "  "}, userCookie, userCSRF)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty query sql status = %d, want 400", rec.Code)
	}

	// Data browse returns the seeded rows in a read-only query, and offset
	// paginates past the first row.
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/query", connectionID),
		map[string]any{"sql": fmt.Sprintf("SELECT * FROM %s", table), "limit": 10}, userCookie, userCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("query status = %d, body = %s", rec.Code, rec.Body.String())
	}
	result := decode[api.QueryResult](t, rec)
	if len(result.Rows) != 3 {
		t.Fatalf("query returned %d rows, want 3", len(result.Rows))
	}
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/query", connectionID),
		map[string]any{"sql": fmt.Sprintf("SELECT * FROM %s", table), "limit": 10, "offset": 1}, userCookie, userCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("offset query status = %d", rec.Code)
	}
	offsetRows := decode[api.QueryResult](t, rec)
	if len(offsetRows.Rows) != 2 {
		t.Fatalf("offset query returned %d rows, want 2", len(offsetRows.Rows))
	}

	// Writes are rejected by the read-only transaction.
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/query", connectionID),
		map[string]any{"sql": "CREATE TABLE should_fail_xyz ()"}, userCookie, userCSRF)
	if rec.Code == http.StatusOK {
		t.Fatal("write through viewer query should not succeed")
	}

	// A user with no grant is denied.
	stranger, strangerPlain := env.createUserDirect("stranger", false, true)
	strangerCookie, _ := env.login(stranger.Username, strangerPlain)
	rec = env.do(http.MethodGet, fmt.Sprintf("/api/databases/%d/schema", connectionID), nil, strangerCookie, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("schema for ungranted user status = %d, want 403", rec.Code)
	}

	// Platform admin without a grant is also denied: admin is not a DB tier.
	rec = env.do(http.MethodGet, fmt.Sprintf("/api/databases/%d/schema", connectionID), nil, adminCookie, adminCSRF)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("schema for admin without grant status = %d, want 403", rec.Code)
	}

	// Unauthenticated requests are rejected.
	rec = env.do(http.MethodGet, fmt.Sprintf("/api/databases/%d/schema", connectionID), nil, nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("schema unauthenticated status = %d, want 401", rec.Code)
	}
}

func TestMigrationLifecycle(t *testing.T) {
	env := newTestEnv(t)
	connectionID, userCookie, userCSRF, adminCookie, adminCSRF := env.newGrantedTarget("view_edit")

	table := strings.ReplaceAll(unique("migtable"), "-", "_")
	t.Cleanup(func() {
		_, _ = env.adminDB.Exec("DROP TABLE IF EXISTS " + table)
	})

	// Create a draft migration; versions must be linear.
	rec := env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations", connectionID),
		map[string]any{"sql_up": fmt.Sprintf("CREATE TABLE %s (id int)", table), "sql_down": fmt.Sprintf("DROP TABLE %s", table)}, userCookie, userCSRF)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create migration status = %d, body = %s", rec.Code, rec.Body.String())
	}
	first := decode[api.Migration](t, rec)
	if first.Version != 1 || first.Status != "draft" {
		t.Fatalf("first migration = %+v, want version 1 draft", first)
	}

	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations", connectionID),
		map[string]any{"sql_up": "SELECT 1"}, userCookie, userCSRF)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create second migration status = %d", rec.Code)
	}
	second := decode[api.Migration](t, rec)
	if second.Version != 2 {
		t.Fatalf("second migration version = %d, want 2", second.Version)
	}

	// Empty sql_up is rejected before touching the database.
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations", connectionID),
		map[string]any{"sql_up": "   "}, userCookie, userCSRF)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty migration sql_up status = %d, want 400", rec.Code)
	}

	// Drafts can be edited; submit the second one for review.
	rec = env.do(http.MethodPatch, fmt.Sprintf("/api/databases/%d/migrations/%d", connectionID, second.Id),
		map[string]any{"sql_up": "SELECT 2"}, userCookie, userCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("update draft migration status = %d, body = %s", rec.Code, rec.Body.String())
	}
	updated := decode[api.Migration](t, rec)
	if updated.SqlUp != "SELECT 2" {
		t.Fatalf("updated sql_up = %q, want SELECT 2", updated.SqlUp)
	}

	// Approvals need a group leader; create one and make them leader of
	// their group before submitting (no admin API for roles yet, so the
	// role is set via ent directly in the grantSetup helper).
	leader, leaderPlain := env.createUserDirect("leader", false, true)
	env.grantSetup(adminCookie, adminCSRF, connectionID, leader.ID, "view_edit", true)
	leaderCookie, leaderCSRF := env.login(leader.Username, leaderPlain)

	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations/%d/submit", connectionID, second.Id),
		nil, userCookie, userCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("submit migration status = %d, body = %s", rec.Code, rec.Body.String())
	}
	submitted := decode[api.Migration](t, rec)
	if submitted.Status != "pending_review" {
		t.Fatalf("submitted migration status = %s, want pending_review", submitted.Status)
	}
	if len(submitted.Executions) != 1 || submitted.Executions[0].Kind != "up" {
		t.Fatalf("submitted migration executions = %+v, want one up execution", submitted.Executions)
	}
	if len(submitted.Approvals) == 0 {
		t.Fatalf("submitted migration has no approval rows")
	}

	// The submitter approves their own group: the user is now group 1's
	// leader (newGrantedTarget), and the leader user's group also granted.
	// Approving one of the two rows still leaves one pending; the second
	// approval (below, by the leader user) enqueues.
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/approvals/%d/approve", submitted.Approvals[0].Id),
		nil, userCookie, userCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve migration status = %d, body = %s", rec.Code, rec.Body.String())
	}
	partial := decode[map[string]any](t, rec)
	if partial["status"] != "pending_review" {
		t.Fatalf("after first approval migration status = %v, want pending_review", partial["status"])
	}
	if partial["job_id"] != nil {
		t.Fatalf("approval with pending groups must not enqueue a job")
	}

	// The second group's leader approves: now every required leader is in.
	var secondApprovalID int
	for _, a := range submitted.Approvals {
		if a.Id != submitted.Approvals[0].Id {
			secondApprovalID = a.Id
		}
	}
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/approvals/%d/approve", secondApprovalID),
		nil, leaderCookie, leaderCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve migration status = %d, body = %s", rec.Code, rec.Body.String())
	}
	approved := decode[map[string]any](t, rec)
	if approved["status"] != "queued" {
		t.Fatalf("approved migration status = %v, want queued", approved["status"])
	}
	if approved["job_id"] == nil {
		t.Fatalf("full approval did not enqueue a job")
	}
	jobID := int(approved["job_id"].(float64))

	// The job exists and stays queued (no worker in tests), execution queued.
	rec = env.do(http.MethodGet, fmt.Sprintf("/api/jobs/%d", jobID), nil, userCookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get job status = %d, body = %s", rec.Code, rec.Body.String())
	}
	job := decode[api.Job](t, rec)
	if job.Status != "queued" {
		t.Fatalf("job status = %s, want queued", job.Status)
	}
	if job.ExecutionId != submitted.Executions[0].Id {
		t.Fatalf("job execution id = %d, want %d", job.ExecutionId, submitted.Executions[0].Id)
	}

	// A non-leader cannot approve.
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/approvals/%d/approve", submitted.Approvals[0].Id),
		nil, userCookie, userCSRF)
	if rec.Code != http.StatusForbidden && rec.Code != http.StatusConflict {
		t.Fatalf("non-leader approve status = %d, want 403 or 409", rec.Code)
	}

	// Drive the worker logic by direct ent writes: execution + job success.
	ctx := context.Background()
	_, err := env.client.MigrationExecution.UpdateOneID(submitted.Executions[0].Id).
		SetStatus(migrationexecution.StatusSuccess).
		SetSQLExecuted("SELECT 2").
		SetFinishedAt(time.Now().UTC()).
		Save(ctx)
	if err != nil {
		t.Fatalf("force execution success: %v", err)
	}
	_, err = env.client.MigrationJob.UpdateOneID(jobID).
		SetStatus(migrationjob.StatusSuccess).
		SetFinishedAt(time.Now().UTC()).
		Save(ctx)
	if err != nil {
		t.Fatalf("force job success: %v", err)
	}
	_, _ = env.client.Migration.UpdateOneID(second.Id).SetStatus(migration.StatusSuccess).Save(ctx)

	// A failing migration is recorded as failed (driven by direct writes to
	// mirror what the worker records).
	badTable := strings.ReplaceAll(unique("badtable"), "-", "_")
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations", connectionID),
		map[string]any{"sql_up": fmt.Sprintf("CREATE TABLES %s ()", badTable), "sql_down": fmt.Sprintf("DROP TABLE %s", badTable)}, userCookie, userCSRF)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create failing migration status = %d", rec.Code)
	}
	bad := decode[api.Migration](t, rec)
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations/%d/submit", connectionID, bad.Id),
		nil, userCookie, userCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("submit failing migration status = %d, body = %s", rec.Code, rec.Body.String())
	}
	badSubmitted := decode[api.Migration](t, rec)
	// Both granting groups must approve; the submitter owns group 1, the
	// extra leader owns group 2.
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/approvals/%d/approve", badSubmitted.Approvals[0].Id),
		nil, userCookie, userCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve failing migration status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var badSecondID int
	for _, a := range badSubmitted.Approvals {
		if a.Id != badSubmitted.Approvals[0].Id {
			badSecondID = a.Id
		}
	}
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/approvals/%d/approve", badSecondID),
		nil, leaderCookie, leaderCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve failing migration status = %d, body = %s", rec.Code, rec.Body.String())
	}
	badApproved := decode[map[string]any](t, rec)
	badJobID := int(badApproved["job_id"].(float64))
	_, err = env.client.MigrationExecution.UpdateOneID(badSubmitted.Executions[0].Id).
		SetStatus(migrationexecution.StatusFailed).
		SetError("syntax error").
		SetFinishedAt(time.Now().UTC()).
		Save(ctx)
	if err != nil {
		t.Fatalf("force execution failed: %v", err)
	}
	_, _ = env.client.MigrationJob.UpdateOneID(badJobID).SetStatus(migrationjob.StatusFailed).SetError("syntax error").Save(ctx)
	_, _ = env.client.Migration.UpdateOneID(bad.Id).SetStatus(migration.StatusFailed).Save(ctx)

	rec = env.do(http.MethodGet, fmt.Sprintf("/api/databases/%d/migrations", connectionID), nil, userCookie, "")
	history := decode[[]api.Migration](t, rec)
	failedStatus := ""
	for _, m := range history {
		if m.Id == bad.Id {
			failedStatus = string(m.Status)
		}
	}
	if failedStatus != "failed" {
		t.Fatalf("failed migration status = %q, want failed", failedStatus)
	}

	// A draft can still be edited and deleted; executed ones cannot.
	rec = env.do(http.MethodPatch, fmt.Sprintf("/api/databases/%d/migrations/%d", connectionID, first.Id),
		map[string]any{"sql_up": "SELECT 1"}, userCookie, userCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("update draft migration status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = env.do(http.MethodPatch, fmt.Sprintf("/api/databases/%d/migrations/%d", connectionID, second.Id),
		map[string]any{"sql_up": "SELECT 3"}, userCookie, userCSRF)
	if rec.Code != http.StatusConflict {
		t.Fatalf("update executed migration status = %d, want 409", rec.Code)
	}
	rec = env.do(http.MethodDelete, fmt.Sprintf("/api/databases/%d/migrations/%d", connectionID, first.Id),
		nil, userCookie, userCSRF)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete draft migration status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// Rollback flow: submit the first migration (recreated as draft is gone,
	// so use a fresh rollback-able migration), approve, mark success, then
	// request a rollback and approve it.
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations", connectionID),
		map[string]any{"sql_up": fmt.Sprintf("CREATE TABLE %s (id int)", table), "sql_down": fmt.Sprintf("DROP TABLE %s", table)}, userCookie, userCSRF)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create rollback-able migration status = %d", rec.Code)
	}
	third := decode[api.Migration](t, rec)
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations/%d/submit", connectionID, third.Id),
		nil, userCookie, userCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("submit third migration status = %d, body = %s", rec.Code, rec.Body.String())
	}
	thirdSubmitted := decode[api.Migration](t, rec)
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/approvals/%d/approve", thirdSubmitted.Approvals[0].Id),
		nil, userCookie, userCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve third migration status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var thirdSecondID int
	for _, a := range thirdSubmitted.Approvals {
		if a.Id != thirdSubmitted.Approvals[0].Id {
			thirdSecondID = a.Id
		}
	}
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/approvals/%d/approve", thirdSecondID),
		nil, leaderCookie, leaderCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve third migration status = %d, body = %s", rec.Code, rec.Body.String())
	}
	thirdApproved := decode[map[string]any](t, rec)
	thirdJobID := int(thirdApproved["job_id"].(float64))
	_, err = env.client.MigrationExecution.UpdateOneID(thirdSubmitted.Executions[0].Id).
		SetStatus(migrationexecution.StatusSuccess).
		SetSQLExecuted(fmt.Sprintf("CREATE TABLE %s (id int)", table)).
		SetFinishedAt(time.Now().UTC()).
		Save(ctx)
	if err != nil {
		t.Fatalf("force third execution success: %v", err)
	}
	_, _ = env.client.MigrationJob.UpdateOneID(thirdJobID).SetStatus(migrationjob.StatusSuccess).Save(ctx)
	_, _ = env.client.Migration.UpdateOneID(third.Id).SetStatus(migration.StatusSuccess).Save(ctx)

	// Now the migration is the latest active one and reversible.
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations/%d/rollback", connectionID, third.Id),
		nil, userCookie, userCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("request rollback status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rollback := decode[api.Migration](t, rec)
	if rollback.Status != "pending_review" {
		t.Fatalf("rollback migration status = %s, want pending_review", rollback.Status)
	}
	var rollbackExec api.Execution
	for _, e := range rollback.Executions {
		if e.Kind == "rollback" {
			rollbackExec = e
		}
	}
	if rollbackExec.Kind != "rollback" || rollbackExec.RevertedExecutionId == nil {
		t.Fatalf("rollback execution = %+v, want kind rollback with reverted_execution_id", rollbackExec)
	}

	// hide_rollback filter: mark the rollback success, then the up execution
	// and the rollback pair should hide while failed attempts stay visible.
	_, err = env.client.MigrationExecution.UpdateOneID(rollbackExec.Id).
		SetStatus(migrationexecution.StatusSuccess).
		SetFinishedAt(time.Now().UTC()).
		Save(ctx)
	if err != nil {
		t.Fatalf("force rollback success: %v", err)
	}
	_, _ = env.client.Migration.UpdateOneID(third.Id).SetStatus(migration.StatusCancelled).Save(ctx)

	rec = env.do(http.MethodGet, fmt.Sprintf("/api/databases/%d/migrations?hide_rollback=true", connectionID), nil, userCookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list with hide_rollback status = %d", rec.Code)
	}
	filtered := decode[[]api.Migration](t, rec)
	for _, m := range filtered {
		if m.Id != third.Id {
			continue
		}
		for _, e := range m.Executions {
			if rollbackExec.RevertedExecutionId != nil && e.Id == *rollbackExec.RevertedExecutionId {
				t.Fatalf("hide_rollback=true still shows cancelled execution %d", e.Id)
			}
			if e.Id == rollbackExec.Id {
				t.Fatalf("hide_rollback=true still shows the rollback execution %d", e.Id)
			}
		}
	}

	// Forward after rollback: the migration left active state, so forward is
	// allowed and creates a forward execution.
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations/%d/forward", connectionID, third.Id),
		nil, userCookie, userCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("request forward status = %d, body = %s", rec.Code, rec.Body.String())
	}
	forwarded := decode[api.Migration](t, rec)
	forwardKind := ""
	for _, e := range forwarded.Executions {
		if e.Status == "pending_review" {
			forwardKind = string(e.Kind)
		}
	}
	if forwardKind != "forward" {
		t.Fatalf("forward execution kind = %q, want forward", forwardKind)
	}

	// History is visible to the granted user (view_edit).
	rec = env.do(http.MethodGet, fmt.Sprintf("/api/databases/%d/migrations", connectionID), nil, userCookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list migrations status = %d", rec.Code)
	}
	history = decode[[]api.Migration](t, rec)
	for _, m := range history {
		if m.AuthorUsername == "" {
			t.Fatalf("migration %d has no author username", m.Id)
		}
		for _, e := range m.Executions {
			if e.ActorUsername == "" {
				t.Fatalf("execution %d has no actor username", e.Id)
			}
		}
	}

	// A view_only user can read history but cannot create or submit.
	viewOnlyUser, viewOnlyPlain := env.createUserDirect("viewer", false, true)
	env.grantSetup(adminCookie, adminCSRF, connectionID, viewOnlyUser.ID, "view_only")
	viewOnlyCookie, viewOnlyCSRF := env.login(viewOnlyUser.Username, viewOnlyPlain)

	rec = env.do(http.MethodGet, fmt.Sprintf("/api/databases/%d/migrations", connectionID), nil, viewOnlyCookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("view_only list migrations status = %d, want 200", rec.Code)
	}
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations", connectionID),
		map[string]any{"sql_up": "SELECT 1"}, viewOnlyCookie, viewOnlyCSRF)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("view_only create migration status = %d, want 403", rec.Code)
	}
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations/%d/submit", connectionID, third.Id),
		nil, viewOnlyCookie, viewOnlyCSRF)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("view_only submit migration status = %d, want 403", rec.Code)
	}
	rec = env.do(http.MethodPatch, fmt.Sprintf("/api/databases/%d/migrations/%d", connectionID, third.Id),
		map[string]any{"sql_up": "SELECT 1"}, viewOnlyCookie, viewOnlyCSRF)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("view_only update migration status = %d, want 403", rec.Code)
	}
}
