package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"Orpbal/internal/api"
)

func TestAdminAuthorization(t *testing.T) {
	e := newTestEnv(t)
	admin, adminPlain := e.createUserDirect("admin", true, true)
	user, userPlain := e.createUserDirect("user", false, true)

	adminCookie, adminCSRF := e.login(admin.Username, adminPlain)
	userCookie, _ := e.login(user.Username, userPlain)

	rec := e.do(http.MethodGet, "/api/admin/users", nil, userCookie, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin status = %d, want 403", rec.Code)
	}
	rec = e.do(http.MethodGet, "/api/me/databases", nil, userCookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("non-admin databases status = %d, want 200", rec.Code)
	}
	rec = e.do(http.MethodGet, "/api/admin/users", nil, adminCookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin status = %d, want 200", rec.Code)
	}
	_ = adminCSRF
}

func TestAdminUserLifecycle(t *testing.T) {
	e := newTestEnv(t)
	admin, adminPlain := e.createUserDirect("admin", true, true)
	cookie, csrfToken := e.login(admin.Username, adminPlain)

	username := unique("person")
	rec := e.do(http.MethodPost, "/api/admin/users", map[string]any{
		"username": username,
		"password": "User-Pass1!",
	}, cookie, csrfToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user status = %d, body = %s", rec.Code, rec.Body.String())
	}
	created := decode[api.User](t, rec)

	rec = e.do(http.MethodPost, "/api/admin/users", map[string]any{
		"username": username,
		"password": "User-Pass1!",
	}, cookie, csrfToken)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate user status = %d, want 409", rec.Code)
	}

	rec = e.do(http.MethodPatch, "/api/admin/users/"+strconv.Itoa(admin.ID), map[string]any{
		"is_active": false,
	}, cookie, csrfToken)
	if rec.Code != http.StatusConflict {
		t.Fatalf("deactivate last admin status = %d, want 409, body = %s", rec.Code, rec.Body.String())
	}
	if body := decode[api.ErrorResponse](t, rec); body.Error.Code != "last_admin" {
		t.Fatalf("last admin code = %q", body.Error.Code)
	}

	rec = e.do(http.MethodPatch, "/api/admin/users/"+strconv.Itoa(created.Id), map[string]any{
		"is_active": false,
	}, cookie, csrfToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("deactivate status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec := e.loginStatus(username, "User-Pass1!"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("inactive login status = %d, want 401", rec.Code)
	}

	rec = e.do(http.MethodPatch, "/api/admin/users/"+strconv.Itoa(created.Id), map[string]any{
		"is_active": true,
	}, cookie, csrfToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("reactivate status = %d, body = %s", rec.Code, rec.Body.String())
	}
	targetCookie, _ := e.login(username, "User-Pass1!")

	rec = e.do(http.MethodPatch, "/api/admin/users/"+strconv.Itoa(created.Id), map[string]any{
		"is_platform_admin": true,
	}, cookie, csrfToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("promote status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec := e.do(http.MethodGet, "/api/auth/session", nil, targetCookie, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("promoted user's old session status = %d, want 401", rec.Code)
	}

	rec = e.do(http.MethodPost, "/api/admin/users/"+strconv.Itoa(created.Id)+"/password", map[string]any{
		"new_password": "Reset-Pass1!",
	}, cookie, csrfToken)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("reset password status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec := e.loginStatus(username, "User-Pass1!"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("old password after reset status = %d, want 401", rec.Code)
	}
	if rec := e.loginStatus(username, "Reset-Pass1!"); rec.Code != http.StatusOK {
		t.Fatalf("new password after reset status = %d, want 200", rec.Code)
	}
}

func TestGroupsGrantsAndEffectiveAccess(t *testing.T) {
	e := newTestEnv(t)
	admin, adminPlain := e.createUserDirect("admin", true, true)
	user, userPlain := e.createUserDirect("user", false, true)
	adminCookie, adminCSRF := e.login(admin.Username, adminPlain)

	rec := e.createConnection(adminCookie, adminCSRF, testTargetConnection(t))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create connection status = %d, body = %s", rec.Code, rec.Body.String())
	}
	connection := e.trackConnection(rec)

	group := func(name string) api.Group {
		rec := e.do(http.MethodPost, "/api/admin/groups", map[string]any{
			"name":        unique(name),
			"description": "test group",
		}, adminCookie, adminCSRF)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create group status = %d, body = %s", rec.Code, rec.Body.String())
		}
		created := decode[api.Group](t, rec)
		return created
	}

	viewers := group("viewers")
	rec = e.do(http.MethodPut, "/api/admin/groups/"+strconv.Itoa(viewers.Id)+"/members/"+strconv.Itoa(user.ID), nil, adminCookie, adminCSRF)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("add member status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = e.do(http.MethodPut, "/api/admin/groups/"+strconv.Itoa(viewers.Id)+"/grants/"+strconv.Itoa(connection.Id), map[string]any{
		"access_level": "view_only",
	}, adminCookie, adminCSRF)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("set grant status = %d, body = %s", rec.Code, rec.Body.String())
	}

	userCookie, _ := e.login(user.Username, userPlain)
	rec = e.do(http.MethodGet, "/api/me/databases", nil, userCookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("my databases status = %d, body = %s", rec.Code, rec.Body.String())
	}
	databases := decode[[]api.MyDatabase](t, rec)
	if len(databases) != 1 || databases[0].AccessLevel != api.ViewOnly {
		t.Fatalf("databases = %+v, want one view_only", databases)
	}

	editors := group("editors")
	rec = e.do(http.MethodPut, "/api/admin/groups/"+strconv.Itoa(editors.Id)+"/members/"+strconv.Itoa(user.ID), nil, adminCookie, adminCSRF)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("add editor member status = %d", rec.Code)
	}
	rec = e.do(http.MethodPut, "/api/admin/groups/"+strconv.Itoa(editors.Id)+"/grants/"+strconv.Itoa(connection.Id), map[string]any{
		"access_level": "view_edit",
	}, adminCookie, adminCSRF)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("set editor grant status = %d", rec.Code)
	}

	rec = e.do(http.MethodGet, "/api/me/databases", nil, userCookie, "")
	databases = decode[[]api.MyDatabase](t, rec)
	if len(databases) != 1 || databases[0].AccessLevel != api.ViewEdit {
		t.Fatalf("databases after second grant = %+v, want view_edit", databases)
	}

	rec = e.do(http.MethodGet, "/api/admin/groups/"+strconv.Itoa(editors.Id), nil, adminCookie, "")
	detail := decode[api.GroupDetail](t, rec)
	if len(detail.Members) != 1 || len(detail.Grants) != 1 {
		t.Fatalf("group detail = %d members, %d grants; want 1,1", len(detail.Members), len(detail.Grants))
	}

	rec = e.do(http.MethodPatch, "/api/admin/databases/"+strconv.Itoa(connection.Id), map[string]any{
		"is_enabled": false,
	}, adminCookie, adminCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable connection status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = e.do(http.MethodGet, "/api/me/databases", nil, userCookie, "")
	if databases := decode[[]api.MyDatabase](t, rec); len(databases) != 0 {
		t.Fatalf("disabled connection still listed: %+v", databases)
	}

	rec = e.do(http.MethodPatch, "/api/admin/databases/"+strconv.Itoa(connection.Id), map[string]any{
		"is_enabled": true,
	}, adminCookie, adminCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable connection status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = e.do(http.MethodGet, "/api/me/databases", nil, userCookie, "")
	if databases := decode[[]api.MyDatabase](t, rec); len(databases) != 1 || databases[0].AccessLevel != api.ViewEdit {
		t.Fatalf("enabled connection databases = %+v", databases)
	}

	rec = e.do(http.MethodDelete, "/api/admin/groups/"+strconv.Itoa(editors.Id)+"/members/"+strconv.Itoa(user.ID), nil, adminCookie, adminCSRF)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("remove member status = %d", rec.Code)
	}
	rec = e.do(http.MethodGet, "/api/me/databases", nil, userCookie, "")
	if databases := decode[[]api.MyDatabase](t, rec); len(databases) != 1 || databases[0].AccessLevel != api.ViewOnly {
		t.Fatalf("after editor removal databases = %+v, want view_only", databases)
	}

	rec = e.do(http.MethodDelete, "/api/admin/groups/"+strconv.Itoa(editors.Id), nil, adminCookie, adminCSRF)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete group status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = e.do(http.MethodGet, "/api/admin/groups/"+strconv.Itoa(editors.Id), nil, adminCookie, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("deleted group status = %d, want 404", rec.Code)
	}
	rec = e.do(http.MethodGet, "/api/admin/grants?group_id="+strconv.Itoa(editors.Id), nil, adminCookie, "")
	if grants := decode[[]api.Grant](t, rec); len(grants) != 0 {
		t.Fatalf("deleted group grants = %+v", grants)
	}

	rec = e.do(http.MethodDelete, "/api/admin/groups/"+strconv.Itoa(viewers.Id), nil, adminCookie, adminCSRF)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete viewer group status = %d", rec.Code)
	}
	rec = e.do(http.MethodGet, "/api/me/databases", nil, userCookie, "")
	if databases := decode[[]api.MyDatabase](t, rec); len(databases) != 0 {
		t.Fatalf("databases after group deletion = %+v", databases)
	}
}

func TestConnectionVerificationAndSecrets(t *testing.T) {
	e := newTestEnv(t)
	admin, plain := e.createUserDirect("admin", true, true)
	cookie, csrfToken := e.login(admin.Username, plain)

	bad := testTargetConnection(t)
	bad["host"] = "127.0.0.1"
	bad["port"] = 1
	rec := e.createConnection(cookie, csrfToken, bad)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad connection status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if body := decode[api.ErrorResponse](t, rec); body.Error.Code != "connection_failed" {
		t.Fatalf("bad connection code = %q", body.Error.Code)
	}

	rec = e.createConnection(cookie, csrfToken, testTargetConnection(t))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create connection status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "password_ciphertext") {
		t.Fatal("connection response contains ciphertext")
	}
	connection := e.trackConnection(rec)
	if !connection.HasPassword {
		t.Fatal("connection has no password flag")
	}

	rec = e.do(http.MethodGet, "/api/admin/databases/"+strconv.Itoa(connection.Id), nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get connection status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "password_ciphertext") {
		t.Fatal("get connection response contains ciphertext")
	}

	rec = e.do(http.MethodPatch, "/api/admin/databases/"+strconv.Itoa(connection.Id), map[string]any{
		"name": unique("renamed"),
	}, cookie, csrfToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename connection status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = e.do(http.MethodPatch, "/api/admin/databases/"+strconv.Itoa(connection.Id), map[string]any{
		"host":       "127.0.0.1",
		"port":       1,
		"is_enabled": true,
	}, cookie, csrfToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad update status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// The failed update must not have persisted the bad host. The test database
	// itself runs on 127.0.0.1, so port is the field that distinguishes the bad values.
	stored, err := e.client.DatabaseConnection.Get(context.Background(), connection.Id)
	if err != nil {
		t.Fatalf("get stored connection: %v", err)
	}
	if stored.Port == 1 {
		t.Fatal("failed verification persisted the new host")
	}
}
