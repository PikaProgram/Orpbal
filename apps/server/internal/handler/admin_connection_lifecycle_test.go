package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"Orpbal/ent/databasegrant"
	"Orpbal/ent/migration"
)

// TestDatabaseConnectionLifecycle covers the admin connection test endpoint
// and the guarded delete: deletable while unused, refused once migration
// history exists, grants removed on delete.
func TestDatabaseConnectionLifecycle(t *testing.T) {
	env := newTestEnv(t)
	admin, plain := env.createUserDirect("connadmin", true, true)
	adminCookie, adminCSRF := env.login(admin.Username, plain)

	// Create a reachable connection (the test PostgreSQL server itself).
	rec := env.createConnection(adminCookie, adminCSRF, testTargetConnection(t))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create connection status = %d, body = %s", rec.Code, rec.Body.String())
	}
	connection := env.trackConnection(rec)

	// Grant a group so the delete must remove the grant as well.
	rec = env.do(http.MethodPost, "/api/admin/groups",
		map[string]any{"name": unique("conn-group")}, adminCookie, adminCSRF)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create group status = %d, body = %s", rec.Code, rec.Body.String())
	}
	group := decode[map[string]any](t, rec)
	groupID := int(group["id"].(float64))
	rec = env.do(http.MethodPut, fmt.Sprintf("/api/admin/groups/%d/grants/%d", groupID, connection.Id),
		map[string]any{"access_level": "view_only"}, adminCookie, adminCSRF)
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated && rec.Code != http.StatusNoContent {
		t.Fatalf("create grant status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// Test endpoint: stored credentials, live ping.
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/admin/databases/%d/test", connection.Id),
		nil, adminCookie, adminCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("test connection status = %d, body = %s", rec.Code, rec.Body.String())
	}
	result := decode[map[string]any](t, rec)
	if result["reachable"] != true {
		t.Fatalf("test connection result = %v, want reachable true", result)
	}

	// Delete while unused succeeds.
	rec = env.do(http.MethodDelete, fmt.Sprintf("/api/admin/databases/%d", connection.Id),
		nil, adminCookie, adminCSRF)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete unused connection status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = env.do(http.MethodGet, fmt.Sprintf("/api/admin/databases/%d", connection.Id),
		nil, adminCookie, adminCSRF)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get deleted connection status = %d, want 404", rec.Code)
	}
	grants, err := env.client.DatabaseGrant.Query().
		Where(databasegrant.ConnectionID(connection.Id)).
		Count(context.Background())
	if err != nil {
		t.Fatalf("count grants after delete: %v", err)
	}
	if grants != 0 {
		t.Fatalf("grants after delete = %d, want 0", grants)
	}

	// A connection with migration history is refused so the linear history
	// never loses its target.
	rec = env.createConnection(adminCookie, adminCSRF, testTargetConnection(t))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create second connection status = %d", rec.Code)
	}
	second := env.trackConnection(rec)
	author, _ := env.createUserDirect("connauthor", false, true)
	_, err = env.client.Migration.Create().
		SetConnectionID(second.Id).
		SetVersion(1).
		SetSQLUp("SELECT 1").
		SetStatus(migration.StatusDraft).
		SetAuthorID(author.ID).
		Save(context.Background())
	if err != nil {
		t.Fatalf("seed migration: %v", err)
	}
	rec = env.do(http.MethodDelete, fmt.Sprintf("/api/admin/databases/%d", second.Id),
		nil, adminCookie, adminCSRF)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete connection with migrations status = %d, want 409", rec.Code)
	}
	rec = env.do(http.MethodGet, fmt.Sprintf("/api/admin/databases/%d", second.Id),
		nil, adminCookie, adminCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("connection must survive refused delete, status = %d", rec.Code)
	}
}

// TestDatabaseConnectionTestUnreachable reports a redacted failure for a
// target that is not listening instead of a 500.
func TestDatabaseConnectionTestUnreachable(t *testing.T) {
	env := newTestEnv(t)
	admin, plain := env.createUserDirect("connadmin2", true, true)
	adminCookie, adminCSRF := env.login(admin.Username, plain)

	// Create a reachable connection, then point it at a dead port with
	// is_enabled=false (PATCH skips verification for disabled targets).
	rec := env.createConnection(adminCookie, adminCSRF, testTargetConnection(t))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create connection status = %d, body = %s", rec.Code, rec.Body.String())
	}
	connection := env.trackConnection(rec)
	rec = env.do(http.MethodPatch, fmt.Sprintf("/api/admin/databases/%d", connection.Id),
		map[string]any{"host": "127.0.0.1", "port": 1, "is_enabled": false}, adminCookie, adminCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable-and-break connection status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/admin/databases/%d/test", connection.Id),
		nil, adminCookie, adminCSRF)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("test unreachable connection status = %d, want 502, body = %s", rec.Code, rec.Body.String())
	}
	failed := decode[map[string]any](t, rec)
	if reached, ok := failed["reachable"]; ok && reached == true {
		t.Fatalf("unreachable target reported reachable: %v", failed)
	}
}
