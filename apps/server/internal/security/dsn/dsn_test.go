package dsn

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestDSNEncodesCredentials(t *testing.T) {
	cfg := Config{
		Host:         "db.internal",
		Port:         5433,
		DatabaseName: "app db",
		Username:     "user@corp",
		Password:     "p@ss:word/!",
		SSLMode:      "require",
	}
	got, err := cfg.DSN()
	if err != nil {
		t.Fatalf("DSN() error = %v", err)
	}

	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	if u.Scheme != "postgres" {
		t.Fatalf("scheme = %q, want postgres", u.Scheme)
	}
	if u.Host != "db.internal:5433" {
		t.Fatalf("host = %q, want db.internal:5433", u.Host)
	}
	if u.User.Username() != "user@corp" {
		t.Fatalf("username = %q", u.User.Username())
	}
	if password, _ := u.User.Password(); password != "p@ss:word/!" {
		t.Fatalf("password = %q", password)
	}
	if u.Path != "/app db" {
		t.Fatalf("path = %q, want /app db", u.Path)
	}
	if u.Query().Get("sslmode") != "require" {
		t.Fatalf("sslmode = %q", u.Query().Get("sslmode"))
	}
}

func TestDSNDefaults(t *testing.T) {
	cfg := Config{Host: "localhost", DatabaseName: "app", Username: "user"}
	got, err := cfg.DSN()
	if err != nil {
		t.Fatalf("DSN() error = %v", err)
	}
	if !strings.Contains(got, "localhost:5432") {
		t.Fatalf("DSN() = %q, missing default port", got)
	}
	if !strings.Contains(got, "sslmode=disable") {
		t.Fatalf("DSN() = %q, missing default sslmode", got)
	}
	if strings.Contains(got, ":@") {
		t.Fatalf("DSN() = %q, contains an empty password marker", got)
	}
}

func TestDSNRejectsInvalidConfig(t *testing.T) {
	tests := []Config{
		{Port: 5432, DatabaseName: "app", Username: "user"},
		{Host: "localhost", Port: -1, DatabaseName: "app", Username: "user"},
		{Host: "localhost", Port: 70000, DatabaseName: "app", Username: "user"},
		{Host: "localhost", DatabaseName: "", Username: "user"},
		{Host: "localhost", DatabaseName: "app", Username: ""},
		{Host: "localhost", DatabaseName: "app", Username: "user", SSLMode: "prefer"},
	}
	for _, cfg := range tests {
		if _, err := cfg.DSN(); err == nil {
			t.Fatalf("DSN() accepted invalid config %+v", cfg)
		}
	}
}

func TestPingFailureDoesNotLeakPassword(t *testing.T) {
	cfg := Config{
		Host:         "127.0.0.1",
		Port:         1,
		DatabaseName: "app",
		Username:     "user",
		Password:     "sup3r-secret",
	}
	built, err := cfg.DSN()
	if err != nil {
		t.Fatalf("DSN() error = %v", err)
	}
	err = Ping(context.Background(), built)
	if err == nil {
		t.Fatal("Ping() succeeded against a closed port")
	}
	if strings.Contains(err.Error(), "sup3r-secret") {
		t.Fatalf("Ping() error leaked the password: %v", err)
	}
}

func TestPingSuccess(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	if err := Ping(context.Background(), dsn); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
}
