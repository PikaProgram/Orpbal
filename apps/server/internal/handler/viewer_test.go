package handler

import (
	"strings"
	"fmt"
	"net/http"
	"testing"

	"Orpbal/internal/api"
)

// grantSetup wires one group with the given access level over one connection
// and makes user a member of it.
func (e *testEnv) grantSetup(adminCookie *http.Cookie, adminCSRF string, connectionID int, userID int, level string) {
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
	e.grantSetup(adminCookie, adminCSRF, connection.Id, user.ID, level)
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

	// Data browse returns the seeded rows in a read-only query.
	// Empty SQL is rejected.
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/query", connectionID),
		map[string]any{"sql": "  "}, userCookie, userCSRF)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty query sql status = %d, want 400", rec.Code)
	}

	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/query", connectionID),
		map[string]any{"sql": fmt.Sprintf("SELECT * FROM %s", table), "limit": 10}, userCookie, userCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("query status = %d, body = %s", rec.Code, rec.Body.String())
	}
	result := decode[api.QueryResult](t, rec)
	if len(result.Rows) != 3 {
		t.Fatalf("query returned %d rows, want 3", len(result.Rows))
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

	// Create two pending migrations; versions must be linear.
	rec := env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations", connectionID),
		map[string]any{"sql": fmt.Sprintf("CREATE TABLE %s (id int)", table)}, userCookie, userCSRF)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create migration status = %d, body = %s", rec.Code, rec.Body.String())
	}
	first := decode[api.Migration](t, rec)
	if first.Version != 1 || first.Status != "pending" {
		t.Fatalf("first migration = %+v, want version 1 pending", first)
	}

	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations", connectionID),
		map[string]any{"sql": "SELECT 1"}, userCookie, userCSRF)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create second migration status = %d", rec.Code)
	}
	second := decode[api.Migration](t, rec)
	if second.Version != 2 {
		t.Fatalf("second migration version = %d, want 2", second.Version)
	}

	// Empty SQL is rejected before touching the database.
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations", connectionID),
		map[string]any{"sql": "   "}, userCookie, userCSRF)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty migration sql status = %d, want 400", rec.Code)
	}

	// Execute the first migration: it must succeed and record status.
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations/%d/execute", connectionID, first.Id),
		nil, userCookie, userCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("execute migration status = %d, body = %s", rec.Code, rec.Body.String())
	}
	executed := decode[api.Migration](t, rec)
	if executed.Status != "success" {
		t.Fatalf("executed migration status = %s, want success", executed.Status)
	}
	var count int
	if err := env.adminDB.QueryRow(
		fmt.Sprintf("SELECT count(*) FROM information_schema.tables WHERE table_name = '%s'", table),
	).Scan(&count); err != nil {
		t.Fatalf("count fixture table: %v", err)
	}
	if count != 1 {
		t.Fatalf("migration table %s was not created", table)
	}

	// Re-executing is a conflict.
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations/%d/execute", connectionID, first.Id),
		nil, userCookie, userCSRF)
	if rec.Code != http.StatusConflict {
		t.Fatalf("re-execute status = %d, want 409", rec.Code)
	}

	// A failing migration is recorded as failed and rolls back.
	badTable := strings.ReplaceAll(unique("badtable"), "-", "_")
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations", connectionID),
		map[string]any{"sql": fmt.Sprintf("CREATE TABLES %s ()", badTable)}, userCookie, userCSRF)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create failing migration status = %d", rec.Code)
	}
	bad := decode[api.Migration](t, rec)
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations/%d/execute", connectionID, bad.Id),
		nil, userCookie, userCSRF)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("failing migration execute status = %d, want 502", rec.Code)
	}
	rec = env.do(http.MethodGet, fmt.Sprintf("/api/databases/%d/migrations", connectionID), nil, userCookie, "")
	history := decode[[]api.Migration](t, rec)
	failedStatus := ""
	for _, entry := range history {
		if entry.Id == bad.Id {
			failedStatus = string(entry.Status)
		}
	}
	if failedStatus != "failed" {
		t.Fatalf("failed migration status = %q, want failed", failedStatus)
	}
	var exists int
	if err := env.adminDB.QueryRow(
		fmt.Sprintf("SELECT count(*) FROM information_schema.tables WHERE table_name = '%s'", badTable),
	).Scan(&exists); err != nil {
		t.Fatalf("count bad table: %v", err)
	}
	if exists != 0 {
		t.Fatalf("failed migration rolled back but table %s exists", badTable)
	}

	// History is visible to the granted user (view_edit) and lists all versions.
	rec = env.do(http.MethodGet, fmt.Sprintf("/api/databases/%d/migrations", connectionID), nil, userCookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list migrations status = %d", rec.Code)
	}
	history = decode[[]api.Migration](t, rec)
	if len(history) != 3 {
		t.Fatalf("migration history has %d entries, want 3", len(history))
	}

	// A view_only user can read history but cannot create or execute.
	viewOnlyUser, viewOnlyPlain := env.createUserDirect("viewer", false, true)
	env.grantSetup(adminCookie, adminCSRF, connectionID, viewOnlyUser.ID, "view_only")
	viewOnlyCookie, viewOnlyCSRF := env.login(viewOnlyUser.Username, viewOnlyPlain)

	rec = env.do(http.MethodGet, fmt.Sprintf("/api/databases/%d/migrations", connectionID), nil, viewOnlyCookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("view_only list migrations status = %d, want 200", rec.Code)
	}
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations", connectionID),
		map[string]any{"sql": "SELECT 1"}, viewOnlyCookie, viewOnlyCSRF)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("view_only create migration status = %d, want 403", rec.Code)
	}
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/migrations/%d/execute", connectionID, second.Id),
		nil, viewOnlyCookie, viewOnlyCSRF)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("view_only execute migration status = %d, want 403", rec.Code)
	}
}
