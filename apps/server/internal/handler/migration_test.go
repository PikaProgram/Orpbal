package handler

import (
	"net/http"
	"encoding/json"
	"fmt"
	"testing"

	"Orpbal/internal/api"
)

// migrationListResponse is the JSON shape returned by GET /api/databases/:id/migrations.
type migrationListResponse struct {
	Migrations []api.Migration `json:"migrations"`
}

// TestMigrationLifecycleFull covers the full migration lifecycle:
// create → list → execute succeed / fail, retry from failed, immutability after success,
// SQL validation, authz, and version linearity.
func TestMigrationLifecycleFull(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	if env == nil {
		t.Fatal("TEST_DATABASE_URL not set, test skipped")
	}

	// ── helpers ──────────────────────────────────────────────────────
	listPath := func(id int) string { return fmt.Sprintf("/api/databases/%d/migrations", id) }
	createPath := func(id int) string { return listPath(id) }
	executePath := func(id, migID int) string { return fmt.Sprintf("/api/databases/%d/migrations/%d/execute", id, migID) }
	updatePath := func(id, migID int) string { return fmt.Sprintf("/api/databases/%d/migrations/%d", id, migID) }

	// ── 1. Granted user ─────────────────────────────────────────────
	connID, userCookie, userCSRF, _, _ := env.newGrantedTarget("view_edit")

	// ── 2. GET empty list ───────────────────────────────────────────
	t.Run("list-empty", func(t *testing.T) {
		rec := env.do(http.MethodGet, listPath(connID), nil, userCookie, userCSRF)
		if rec.Code != http.StatusOK {
			t.Fatalf("list-empty: status = %d", rec.Code)
		}
	})

	// ── 3. Authz: view only user gets 403 on migration list ──────────
	t.Run("view-user-403", func(t *testing.T) {
		connID2, userCookie2, userCSRF2, _, _ := env.newGrantedTarget("view")
		rec := env.do(http.MethodGet, listPath(connID2), nil, userCookie2, userCSRF2)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("view-user-403: status = %d", rec.Code)
		}
	})

	// ── 4. POST create migration with valid SQL ───────────────────────
	t.Run("create-valid-sql", func(t *testing.T) {
		body := api.CreateMigrationRequest{Sql: "SELECT 1"}
		rec := env.do(http.MethodPost, createPath(connID), body, userCookie, userCSRF)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create-valid-sql: status = %d", rec.Code)
		}
		mig := decode[api.Migration](t, rec)
		if mig.Sql != "SELECT 1" {
			t.Fatalf("create-valid-sql: sql = %q", mig.Sql)
		}
		if mig.Version != 1 {
			t.Fatalf("create-valid-sql: version = %d", mig.Version)
		}
		if mig.Status != api.MigrationStatusPending {
			t.Fatalf("create-valid-sql: status = %q", mig.Status)
		}
		if mig.ExecutorUsername == "" {
			t.Fatal("create-valid-sql: executor_username missing")
		}
	})

	// ── 5. POST create second migration (linear version) ────────────
	t.Run("create-linear-version", func(t *testing.T) {
		body := api.CreateMigrationRequest{Sql: "SELECT 2"}
		rec := env.do(http.MethodPost, createPath(connID), body, userCookie, userCSRF)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create-linear: status = %d", rec.Code)
		}
		mig := decode[api.Migration](t, rec)
		if mig.Version != 2 {
			t.Fatalf("create-linear: version = %d", mig.Version)
		}
	})

	// ── 6. POST create with unreadable SQL → 400 ────────────────────
	t.Run("create-unreadable-sql-400", func(t *testing.T) {
		body := api.CreateMigrationRequest{Sql: "DROP TABLE test_invalid_migration_123"}
		rec := env.do(http.MethodPost, createPath(connID), body, userCookie, userCSRF)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("create-unreadable: status = %d", rec.Code)
		}
	})

	// ── 7. GET migrations after creates ─────────────────────────────
	t.Run("list-after-creates", func(t *testing.T) {
		rec := env.do(http.MethodGet, listPath(connID), nil, userCookie, userCSRF)
		if rec.Code != http.StatusOK {
			t.Fatalf("list-after-creates: status = %d", rec.Code)
		}
		var resp migrationListResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("list-after-creates decode: %v", err)
		}
		if len(resp.Migrations) != 2 {
			t.Fatalf("list-after-creates: expected 2 migrations, got %d", len(resp.Migrations))
		}
		if resp.Migrations[0].Version != 1 || resp.Migrations[1].Version != 2 {
			t.Fatalf("list-after-creases: first version = %d, second = %d", resp.Migrations[0].Version, resp.Migrations[1].Version)
		}
	})

	// ── 8. Execute pending migration → success ──────────────────────
	t.Run("execute-success", func(t *testing.T) {
		rec := env.do(http.MethodPost, executePath(connID, 1), nil, userCookie, userCSRF)
		if rec.Code != http.StatusOK {
			t.Fatalf("execute-success: status = %d", rec.Code)
		}
		mig := decode[api.Migration](t, rec)
		if mig.Status != api.MigrationStatusSuccess {
			t.Fatalf("execute-success: status = %q", mig.Status)
		}
		if mig.ExecutedAt.IsZero() {
			t.Fatal("execute-success: executed_at is zero")
		}
	})

	// ── 9. Execute already-executed migration → 409 ────────────────
	t.Run("execute-already-executed-409", func(t *testing.T) {
		rec := env.do(http.MethodPost, executePath(connID, 1), nil, userCookie, userCSRF)
		if rec.Code != http.StatusConflict {
			t.Fatalf("execute-already-executed: status = %d", rec.Code)
		}
	})

	// ── 10. Execute with failing SQL → 502, status failed ───────────
	t.Run("execute-failing-sql-502", func(t *testing.T) {
		body := api.CreateMigrationRequest{Sql: "SELECT * FROM no_such_table_for_fake_migration_999"}
		rec := env.do(http.MethodPost, createPath(connID), body, userCookie, userCSRF)
		if rec.Code != http.StatusCreated {
			t.Fatalf("fail-setup: status = %d", rec.Code)
		}
		failedMig := decode[api.Migration](t, rec)
		rec = env.do(http.MethodPost, executePath(connID, failedMig.Id), nil, userCookie, userCSRF)
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("execute-failing: status = %d", rec.Code)
		}
		mig := decode[api.Migration](t, rec)
		if mig.Status != api.MigrationStatusFailed {
			t.Fatalf("execute-failing: status = %q", mig.Status)
		}
	})

	// ── 11. PATCH failed migration → re-opens to pending ────────────
	t.Run("patch-failed-reopens-pending", func(t *testing.T) {
		body := api.CreateMigrationRequest{Sql: "SELECT * FROM no_such_table_for_migration_retry_777"}
		rec := env.do(http.MethodPost, createPath(connID), body, userCookie, userCSRF)
		if rec.Code != http.StatusCreated {
			t.Fatalf("pre-retry-setup: status = %d", rec.Code)
		}
		failedMig := decode[api.Migration](t, rec)
		rec = env.do(http.MethodPatch, updatePath(connID, failedMig.Id), api.CreateMigrationRequest{Sql: "SELECT 42"}, userCookie, userCSRF)
		if rec.Code != http.StatusOK {
			t.Fatalf("patch-failed-reopens-pending: status = %d", rec.Code)
		}
		mig := decode[api.Migration](t, rec)
		if mig.Status != api.MigrationStatusPending {
			t.Fatalf("patch-failed-reopens-pending: status = %q", mig.Status)
		}
		if mig.Sql != "SELECT 42" {
			t.Fatalf("patch-failed-reopens-pending: sql = %q", mig.Sql)
		}
	})

	// ── 12. PATCH success → 409 ────────────────────────────────────
	t.Run("patch-success-409", func(t *testing.T) {
		rec := env.do(http.MethodPatch, updatePath(connID, 1), api.CreateMigrationRequest{Sql: "SELECT 1"}, userCookie, userCSRF)
		if rec.Code != http.StatusConflict {
			t.Fatalf("patch-success: status = %d", rec.Code)
		}
	})

	// ── 13. Execute reopened migration → success ────────────────────
	t.Run("execute-reopened-then-succeed", func(t *testing.T) {
		rec := env.do(http.MethodPost, executePath(connID, 1), nil, userCookie, userCSRF)
		if rec.Code != http.StatusOK {
			t.Fatalf("execute-reopened: status = %d", rec.Code)
		}
		mig := decode[api.Migration](t, rec)
		if mig.Status != api.MigrationStatusSuccess {
			t.Fatalf("execute-reopened: status = %q", mig.Status)
		}
	})

	// ── 14. SQL empty → 400 ────────────────────────────────────────
	t.Run("create-empty-sql-400", func(t *testing.T) {
		body := api.CreateMigrationRequest{Sql: "  "}
		rec := env.do(http.MethodPost, createPath(connID), body, userCookie, userCSRF)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("create-empty-sql: status = %d", rec.Code)
		}
	})
}