package dsn

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
)

const PingTimeout = 5 * time.Second

// DSNPrefixMySQL marks stored DSNs for the MySQL engine; Postgres DSNs have no prefix.
const DSNPrefixMySQL = EngineMySQL + "://"

// Engines supported by the connection layer; DatabaseConnection.engine mirrors this.
const (
	EnginePostgres = "postgres"
	EngineMySQL    = "mysql"
)

// DriverForDSN splits a stored DSN into database/sql driver name and the
// actual connect string. MySQL DSNs carry a "mysql://" prefix because the
// driver's native format has no scheme; Postgres URLs are passed through.
func DriverForDSN(dsn string) (driver, connect string) {
	if strings.HasPrefix(dsn, EngineMySQL+"://") {
		return EngineMySQL, strings.TrimPrefix(dsn, EngineMySQL+"://")
	}
	return "postgres", dsn
}

var sslModes = map[string]bool{
	"disable":     true,
	"require":     true,
	"verify-ca":   true,
	"verify-full": true,
}

// Config describes one managed database target; Engine selects the dialect.
type Config struct {
	Engine       string
	Host         string
	Port         int
	DatabaseName string
	Username     string
	Password     string
	SSLMode      string
}

// MySQLDSN builds a go-sql-driver/mysql connect string: user:pass@tcp(host:port)/db.
// SSL: locked internally to encrypted-without-verification for require/*,
// plaintext only for disable.
func (c Config) MySQLDSN() (string, error) {
	if strings.TrimSpace(c.Host) == "" {
		return "", fmt.Errorf("host is required")
	}
	if c.Port == 0 {
		c.Port = 3306
	}
	if c.Port < 1 || c.Port > 65535 {
		return "", fmt.Errorf("port must be between 1 and 65535")
	}
	if c.DatabaseName == "" {
		return "", fmt.Errorf("database name is required")
	}
	if c.Username == "" {
		return "", fmt.Errorf("username is required")
	}
	q := url.Values{}
	q.Set("charset", "utf8mb4")
	if c.SSLMode != "" && c.SSLMode != "disable" {
		q.Set("tls", "skip-verify")
	}
	// shortcut: go-sql-driver performs no percent-decoding, so credentials
	// containing : @ / ( ) cannot be expressed in a MySQL DSN; such a
	// password fails fast at connect. Upgrade if needed.
	host := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	prefix := ""
	if c.Password != "" {
		prefix = c.Username + ":" + c.Password + "@"
	} else {
		prefix = c.Username + "@"
	}
	return EngineMySQL + "://" + prefix + "tcp(" + host + ")/" + c.DatabaseName +
		"?" + q.Encode(), nil
}

// DSN builds a stored connect string for the configured engine.
func (c Config) DSN() (string, error) {
	if c.Engine == "" || c.Engine == EnginePostgres {
		dsn, err := c.postgresDSN()
		return dsn, err
	}
	if c.Engine == EngineMySQL {
		return c.MySQLDSN()
	}
	return "", fmt.Errorf("invalid engine %q", c.Engine)
}

// DSN builds a stored postgres:// URL while counting as the postgres engine.
func (c Config) postgresDSN() (string, error) {
	if strings.TrimSpace(c.Host) == "" {
		return "", fmt.Errorf("host is required")
	}
	if c.Port == 0 {
		c.Port = 5432
	}
	if c.Port < 1 || c.Port > 65535 {
		return "", fmt.Errorf("port must be between 1 and 65535")
	}
	if c.DatabaseName == "" {
		return "", fmt.Errorf("database name is required")
	}
	if c.Username == "" {
		return "", fmt.Errorf("username is required")
	}
	if c.SSLMode == "" {
		c.SSLMode = "disable"
	}
	if !sslModes[c.SSLMode] {
		return "", fmt.Errorf("invalid ssl mode %q", c.SSLMode)
	}

	u := &url.URL{
		Scheme: "postgres",
		Host:   net.JoinHostPort(c.Host, strconv.Itoa(c.Port)),
		Path:   "/" + c.DatabaseName,
	}
	if c.Password == "" {
		u.User = url.User(c.Username)
	} else {
		u.User = url.UserPassword(c.Username, c.Password)
	}
	q := url.Values{}
	q.Set("sslmode", c.SSLMode)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Ping opens a short-lived connection to verify the target is reachable.
// For MySQL it also refuses servers older than 8.0.
func Ping(ctx context.Context, dsn string) error {
	driver, connect := DriverForDSN(dsn)
	ctx, cancel := context.WithTimeout(ctx, PingTimeout)
	defer cancel()

	db, err := sql.Open(driver, connect)
	if err != nil {
		return fmt.Errorf("open target database: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect to target database: %w", err)
	}
	if driver == EngineMySQL {
		if err := CheckMySQL8(db); err != nil {
			return err
		}
	}
	return nil
}

// CheckMySQL8 requires MySQL server version >= 8.0; refused otherwise.
func CheckMySQL8(db *sql.DB) error {
	var version string
	if err := db.QueryRow("SELECT VERSION()").Scan(&version); err != nil {
		return fmt.Errorf("check MySQL version: %w", err)
	}
	major, _, ok := strings.Cut(version, ".")
	if !ok {
		return fmt.Errorf("unexpected MySQL version string %q", version)
	}
	n, err := strconv.Atoi(major)
	if err != nil || n < 8 {
		return fmt.Errorf("mysql server version %q is below the supported 8.0 floor", version)
	}
	return nil
}
