package sqllog

import (
	"errors"
	"testing"

	"github.com/lib/pq"
)

func TestRedactDSNPasswords(t *testing.T) {
	in := "postgres://app:s3cretPw@db.local:5432/appdb?sslmode=disable"
	out := Redact(in)
	if want := "postgres://app:***@db.local:5432/appdb?sslmode=disable"; out != want {
		t.Fatalf("Redact(dsn) = %q, want %q", out, want)
	}
	mysql := "mysql://root:pw123@tcp(127.0.0.1:3306)/db"
	if out := Redact(mysql); out != "mysql://root:***@tcp(127.0.0.1:3306)/db" {
		t.Fatalf("Redact(mysql dsn) = %q", out)
	}
}

func TestRedactCredentialLiteralsInSQL(t *testing.T) {
	in := "ALTER USER app WITH PASSWORD 'hunter2'; INSERT INTO t(token) VALUES ('abc123')"
	out := Redact(in)
	if contains(out, "hunter2") || contains(out, "abc123") {
		t.Fatalf("Redact(sql) leaked secrets: %q", out)
	}
	if !contains(out, "PASSWORD '***'") || !contains(out, "token) VALUES ('***'") {
		t.Fatalf("Redact(sql) = %q; expected masked literals", out)
	}
}

func TestRedactPlainTextUnchanged(t *testing.T) {
	in := "SELECT id FROM users WHERE id > 10 ORDER BY id LIMIT 100"
	if out := Redact(in); out != in {
		t.Fatalf("Redact(plain sql) changed the statement: %q", out)
	}
}

func TestSQLStateFromPqError(t *testing.T) {
	err := &pq.Error{Code: pq.ErrorCode("23505"), Message: "duplicate key"}
	if got := SQLState(err); got != "23505" {
		t.Fatalf("SQLState = %q, want 23505", got)
	}
	wrapped := errors.New("execute statement: " + err.Error())
	// A wrapped-by-text error has no SQLSTATE; must not panic or fabricate one.
	if got := SQLState(wrapped); got != "" {
		t.Fatalf("SQLState(text-wrapped) = %q, want empty", got)
	}
	if got := SQLState(nil); got != "" {
		t.Fatalf("SQLState(nil) = %q", got)
	}
}

func TestLogSQLBoundsLength(t *testing.T) {
	long := "SELECT " + string(make([]byte, 500))
	out := LogSQL(long)
	if len(out) > SQLRedactMax+40 {
		t.Fatalf("LogSQL length = %d, unbounded", len(out))
	}
	if !contains(out, "chars)") {
		t.Fatalf("LogSQL = %q; expected truncation marker", out)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
