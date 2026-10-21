package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"Orpbal/ent"
	"Orpbal/ent/session"
	"Orpbal/internal/api"
	"Orpbal/internal/auth"
	"Orpbal/internal/security/crypto"
	"Orpbal/internal/security/csrf"
	"Orpbal/internal/security/password"
	sessionpolicy "Orpbal/internal/security/session"
	tokenpkg "Orpbal/internal/security/token"

	"github.com/lib/pq"
)

const testOrigin = "http://localhost:4321"

var testSeq atomic.Int64

type testEnv struct {
	t       *testing.T
	client  *ent.Client
	router  http.Handler
	adminDB *sql.DB
	schema  string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	rawDSN := os.Getenv("TEST_DATABASE_URL")
	if rawDSN == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	parsed, err := url.Parse(rawDSN)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	adminDB, err := sql.Open("postgres", rawDSN)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	schema := fmt.Sprintf("test_%d_%d", os.Getpid(), testSeq.Add(1))
	if _, err := adminDB.Exec("CREATE SCHEMA " + pq.QuoteIdentifier(schema)); err != nil {
		_ = adminDB.Close()
		t.Fatalf("create test schema: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()

	client, err := ent.Open("postgres", parsed.String())
	if err != nil {
		_, _ = adminDB.Exec("DROP SCHEMA IF EXISTS " + pq.QuoteIdentifier(schema) + " CASCADE")
		_ = adminDB.Close()
		t.Fatalf("ent.Open() error = %v", err)
	}
	if err := client.Schema.Create(context.Background()); err != nil {
		_ = client.Close()
		_, _ = adminDB.Exec("DROP SCHEMA IF EXISTS " + pq.QuoteIdentifier(schema) + " CASCADE")
		_ = adminDB.Close()
		t.Fatalf("Schema.Create() error = %v", err)
	}

	key := bytes.Repeat([]byte{0x6b}, 32)
	cipher, err := crypto.New(key)
	if err != nil {
		t.Fatalf("crypto.New() error = %v", err)
	}
	csrfManager, err := csrf.New(key)
	if err != nil {
		t.Fatalf("csrf.New() error = %v", err)
	}
	sessions, err := auth.NewService(client, sessionpolicy.Default(), csrfManager)
	if err != nil {
		t.Fatalf("auth.NewService() error = %v", err)
	}
	h := New(client, sessions, cipher, false, zerolog.Nop())

	gin.SetMode(gin.TestMode)
	router := gin.New()
	api.RegisterHandlersWithOptions(router.Group("/api"), h, api.GinServerOptions{
		ErrorHandler: h.ParameterError,
		Middlewares: []api.MiddlewareFunc{
			api.MiddlewareFunc(auth.Authenticate(sessions, false, testOrigin, zerolog.Nop())),
			api.MiddlewareFunc(auth.RequirePlatformAdmin()),
		},
	})

	env := &testEnv{t: t, client: client, router: router, adminDB: adminDB, schema: schema}
	t.Cleanup(env.cleanup)
	return env
}

func (e *testEnv) cleanup() {
	_ = e.client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, _ = e.adminDB.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+pq.QuoteIdentifier(e.schema)+" CASCADE")
	_ = e.adminDB.Close()
}

func unique(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, testSeq.Add(1))
}

func (e *testEnv) createUserDirect(prefix string, admin, active bool) (*ent.PlatformUser, string) {
	e.t.Helper()
	const plain = "Test-Pass1!"
	hash, err := password.Hash(plain)
	if err != nil {
		e.t.Fatalf("password.Hash() error = %v", err)
	}
	user, err := e.client.PlatformUser.Create().
		SetUsername(unique(prefix)).
		SetPasswordHash(hash).
		SetIsPlatformAdmin(admin).
		SetIsActive(active).
		Save(context.Background())
	if err != nil {
		e.t.Fatalf("create user: %v", err)
	}
	return user, plain
}

func (e *testEnv) do(method, path string, body any, cookie *http.Cookie, csrfToken string) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.doWithHeaders(method, path, body, cookie, csrfToken, nil)
}

func (e *testEnv) doWithHeaders(method, path string, body any, cookie *http.Cookie, csrfToken string, headers map[string]string) *httptest.ResponseRecorder {
	e.t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			e.t.Fatalf("json.Marshal() error = %v", err)
		}
		reader = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if csrfToken != "" {
		req.Header.Set("X-CSRF-Token", csrfToken)
	}
	if method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
		req.Header.Set("Origin", testOrigin)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func cookieFrom(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == name && cookie.MaxAge >= 0 {
			return cookie
		}
	}
	return nil
}

func (e *testEnv) login(username, plain string) (*http.Cookie, string) {
	e.t.Helper()
	rec := e.do(http.MethodPost, "/api/auth/login", map[string]string{
		"username": username,
		"password": plain,
	}, nil, "")
	if rec.Code != http.StatusOK {
		e.t.Fatalf("login status = %d, body = %s", rec.Code, rec.Body.String())
	}
	cookie := cookieFrom(rec, auth.CookieName)
	if cookie == nil {
		e.t.Fatal("login did not set a session cookie")
	}
	var body api.Session
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		e.t.Fatalf("decode session: %v", err)
	}
	if body.CsrfToken == "" {
		e.t.Fatal("login response has no CSRF token")
	}
	return cookie, body.CsrfToken
}

func (e *testEnv) loginStatus(username, plain string) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.do(http.MethodPost, "/api/auth/login", map[string]string{
		"username": username,
		"password": plain,
	}, nil, "")
}

func (e *testEnv) sessionUpdate(cookie *http.Cookie) *ent.SessionUpdate {
	return e.client.Session.Update().Where(session.TokenHashEQ(tokenpkg.Hash(cookie.Value)))
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(rec.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return value
}

func testTargetConnection(t *testing.T) map[string]any {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	host, portText, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatalf("split test database host: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse test database port: %v", err)
	}
	plain, _ := parsed.User.Password()
	sslMode := parsed.Query().Get("sslmode")
	if sslMode == "" {
		sslMode = "disable"
	}
	return map[string]any{
		"name":          unique("target"),
		"host":          host,
		"port":          port,
		"database_name": strings.TrimPrefix(parsed.Path, "/"),
		"username":      parsed.User.Username(),
		"password":      plain,
		"ssl_mode":      sslMode,
	}
}

func (e *testEnv) createConnection(adminCookie *http.Cookie, csrfToken string, body map[string]any) *httptest.ResponseRecorder {
	e.t.Helper()
	rec := e.do(http.MethodPost, "/api/admin/databases", body, adminCookie, csrfToken)
	return rec
}

func (e *testEnv) trackConnection(rec *httptest.ResponseRecorder) api.DatabaseConnection {
	e.t.Helper()
	return decode[api.DatabaseConnection](e.t, rec)
}
