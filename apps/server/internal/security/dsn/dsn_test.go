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

func TestMySQLDSN(t *testing.T) {
	got, err := Config{
		Engine:       EngineMySQL,
		Host:         "db.internal",
		Port:         3307,
		DatabaseName: "app",
		Username:     "user",
		Password:     "secret",
		SSLMode:      "require",
	}.DSN()
	if err != nil {
		t.Fatalf("DSN() error = %v", err)
	}
	if got != "mysql://user:secret@tcp(db.internal:3307)/app?charset=utf8mb4&tls=skip-verify" {
		t.Fatalf("DSN() = %q", got)
	}
	driver, connect := DriverForDSN(got)
	if driver != EngineMySQL || connect != "user:secret@tcp(db.internal:3307)/app?charset=utf8mb4&tls=skip-verify" {
		t.Fatalf("DriverForDSN() = (%q, %q)", driver, connect)
	}
}

func TestMySQLDSNDefaults(t *testing.T) {
	got, err := Config{Engine: EngineMySQL, Host: "localhost", DatabaseName: "app", Username: "user"}.DSN()
	if err != nil {
		t.Fatalf("DSN() error = %v", err)
	}
	if !strings.Contains(got, "tcp(localhost:3306)") {
		t.Fatalf("DSN() = %q, missing default port", got)
	}
	if strings.Contains(got, "tls=") {
		t.Fatalf("DSN() = %q, unexpected TLS parameter for sslmode disable", got)
	}
}

func TestMySQLEmptyPasswordOmitted(t *testing.T) {
	got, err := Config{Engine: EngineMySQL, Host: "localhost", DatabaseName: "app", Username: "user", SSLMode: "require"}.DSN()
	if err != nil {
		t.Fatalf("DSN() error = %v", err)
	}
	if strings.Contains(got, ":@") {
		t.Fatalf("DSN() = %q, contains an empty password marker", got)
	}
	if !strings.Contains(got, "tls=skip-verify") {
		t.Fatalf("DSN() = %q, missing TLS parameter for sslmode require", got)
	}
}

func TestDSNRejectsUnknownEngine(t *testing.T) {
	cfg := Config{Engine: "sqlite", Host: "db", Port: 3306, DatabaseName: "app", Username: "user"}
	if _, err := cfg.DSN(); err == nil {
		t.Fatal("DSN() accepted an unknown engine")
	}
}

func TestDriverForDSNPostgresPassthrough(t *testing.T) {
	driver, connect := DriverForDSN("postgres://u@h:5432/db")
	if driver != "postgres" || connect != "postgres://u@h:5432/db" {
		t.Fatalf("DriverForDSN() = (%q, %q)", driver, connect)
	}
}
