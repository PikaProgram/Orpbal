package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"Orpbal/ent/session"
	"Orpbal/internal/api"
	"Orpbal/internal/auth"
	tokenpkg "Orpbal/internal/security/token"
)

func TestLoginSessionAndLogout(t *testing.T) {
	e := newTestEnv(t)
	admin, plain := e.createUserDirect("admin", true, true)

	cookie, csrfToken := e.login(admin.Username, plain)
	if !cookie.HttpOnly {
		t.Fatal("session cookie is not HttpOnly")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie SameSite = %v, want Lax", cookie.SameSite)
	}
	if cookie.Path != "/" {
		t.Fatalf("session cookie Path = %q, want /", cookie.Path)
	}

	rec := e.do(http.MethodGet, "/api/auth/session", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET session status = %d, body = %s", rec.Code, rec.Body.String())
	}
	sessionBody := decode[api.Session](t, rec)
	if sessionBody.User.Username != admin.Username {
		t.Fatalf("session user = %q, want %q", sessionBody.User.Username, admin.Username)
	}
	if sessionBody.CsrfToken != csrfToken {
		t.Fatal("CSRF token changed between login and session fetch")
	}

	rec = e.do(http.MethodGet, "/api/me/databases", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET databases status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if databases := decode[[]api.MyDatabase](t, rec); len(databases) != 0 {
		t.Fatalf("databases = %d, want 0", len(databases))
	}

	rec = e.do(http.MethodPost, "/api/auth/logout", nil, cookie, csrfToken)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = e.do(http.MethodGet, "/api/auth/session", nil, cookie, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("session after logout status = %d, want 401", rec.Code)
	}
}

func TestLoginFailuresAndOrigin(t *testing.T) {
	e := newTestEnv(t)
	user, plain := e.createUserDirect("user", false, true)
	inactive, inactivePlain := e.createUserDirect("inactive", false, false)

	tests := []struct {
		name     string
		username string
		password string
	}{
		{name: "wrong password", username: user.Username, password: "Wrong-Pass1!"},
		{name: "unknown user", username: unique("missing"), password: plain},
		{name: "inactive user", username: inactive.Username, password: inactivePlain},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := e.loginStatus(tt.username, tt.password)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			body := decode[api.ErrorResponse](t, rec)
			if body.Error.Code != "invalid_credentials" {
				t.Fatalf("error code = %q, want invalid_credentials", body.Error.Code)
			}
			if cookieFrom(rec, auth.CookieName) != nil {
				t.Fatal("failed login set a session cookie")
			}
		})
	}

	rec := e.doWithHeaders(http.MethodPost, "/api/auth/login", map[string]string{
		"username": user.Username,
		"password": plain,
	}, nil, "", map[string]string{"Origin": "http://evil.example"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("evil origin status = %d, want 403", rec.Code)
	}

	rec = e.loginStatus(user.Username, plain)
	if rec.Code != http.StatusOK {
		t.Fatalf("allowed origin status = %d, want 200", rec.Code)
	}
}

func TestCSRFProtection(t *testing.T) {
	e := newTestEnv(t)
	user, plain := e.createUserDirect("user", false, true)
	cookie, csrfToken := e.login(user.Username, plain)

	rec := e.do(http.MethodPost, "/api/auth/logout", nil, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing CSRF status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}

	rec = e.do(http.MethodPost, "/api/auth/logout", nil, cookie, "wrong-token")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("wrong CSRF status = %d, want 403, body = %s", rec.Code, rec.Body.String())
	}

	rec = e.do(http.MethodPost, "/api/auth/logout", nil, cookie, csrfToken)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("valid CSRF status = %d, want 204, body = %s", rec.Code, rec.Body.String())
	}
}

func TestSessionIdleAndAbsoluteExpiry(t *testing.T) {
	e := newTestEnv(t)
	user, plain := e.createUserDirect("user", false, true)

	t.Run("idle", func(t *testing.T) {
		cookie, _ := e.login(user.Username, plain)
		count, err := e.sessionUpdate(cookie).SetIdleExpiresAt(time.Now().Add(-time.Minute)).Save(context.Background())
		if err != nil || count != 1 {
			t.Fatalf("expire idle session: count=%d err=%v", count, err)
		}
		rec := e.do(http.MethodGet, "/api/auth/session", nil, cookie, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("idle-expired session status = %d, want 401", rec.Code)
		}
	})

	t.Run("absolute", func(t *testing.T) {
		cookie, _ := e.login(user.Username, plain)
		count, err := e.sessionUpdate(cookie).SetAbsoluteExpiresAt(time.Now().Add(-time.Minute)).Save(context.Background())
		if err != nil || count != 1 {
			t.Fatalf("expire absolute session: count=%d err=%v", count, err)
		}
		rec := e.do(http.MethodGet, "/api/auth/session", nil, cookie, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("absolute-expired session status = %d, want 401", rec.Code)
		}
	})
}

func TestSessionRotationAndGrace(t *testing.T) {
	e := newTestEnv(t)
	user, plain := e.createUserDirect("user", false, true)
	cookie, _ := e.login(user.Username, plain)

	count, err := e.sessionUpdate(cookie).SetRotatedAt(time.Now().Add(-20 * time.Minute)).Save(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("make rotation due: count=%d err=%v", count, err)
	}

	rec := e.do(http.MethodGet, "/api/auth/session", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("rotation request status = %d, body = %s", rec.Code, rec.Body.String())
	}
	newCookie := cookieFrom(rec, auth.CookieName)
	if newCookie == nil {
		t.Fatal("rotation did not set a new session cookie")
	}
	if newCookie.Value == cookie.Value {
		t.Fatal("rotation kept the old token")
	}

	rec = e.do(http.MethodGet, "/api/auth/session", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("old token inside grace status = %d, want 200", rec.Code)
	}

	count, err = e.client.Session.Update().
		Where(session.PreviousTokenHashEQ(tokenpkg.Hash(cookie.Value))).
		SetPreviousValidUntil(time.Now().Add(-time.Minute)).
		Save(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("expire grace: count=%d err=%v", count, err)
	}

	rec = e.do(http.MethodGet, "/api/auth/session", nil, cookie, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("old token after grace status = %d, want 401", rec.Code)
	}
	rec = e.do(http.MethodGet, "/api/auth/session", nil, newCookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("new token status = %d, want 200", rec.Code)
	}
}

func TestPasswordChangeRevokesOtherSessions(t *testing.T) {
	e := newTestEnv(t)
	user, plain := e.createUserDirect("user", false, true)
	cookieA, csrfA := e.login(user.Username, plain)
	cookieB, _ := e.login(user.Username, plain)

	rec := e.do(http.MethodPost, "/api/auth/password", map[string]string{
		"current_password": plain,
		"new_password":     "New-Pass1!",
	}, cookieA, csrfA)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("change password status = %d, body = %s", rec.Code, rec.Body.String())
	}
	newCookie := cookieFrom(rec, auth.CookieName)
	if newCookie == nil {
		t.Fatal("password change did not rotate the session cookie")
	}

	if rec := e.loginStatus(user.Username, plain); rec.Code != http.StatusUnauthorized {
		t.Fatalf("old password status = %d, want 401", rec.Code)
	}
	if rec := e.loginStatus(user.Username, "New-Pass1!"); rec.Code != http.StatusOK {
		t.Fatalf("new password status = %d, want 200", rec.Code)
	}
	if rec := e.do(http.MethodGet, "/api/auth/session", nil, cookieB, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("other session status = %d, want 401", rec.Code)
	}
	if rec := e.do(http.MethodGet, "/api/auth/session", nil, newCookie, ""); rec.Code != http.StatusOK {
		t.Fatalf("rotated current session status = %d, want 200", rec.Code)
	}
}

func TestPasswordChangeRejectsWrongCurrent(t *testing.T) {
	e := newTestEnv(t)
	user, plain := e.createUserDirect("user", false, true)
	cookie, csrfToken := e.login(user.Username, plain)

	rec := e.do(http.MethodPost, "/api/auth/password", map[string]string{
		"current_password": "Wrong-Pass1!",
		"new_password":     "New-Pass1!",
	}, cookie, csrfToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}
}
