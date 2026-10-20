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

	_ "github.com/lib/pq"
)

const PingTimeout = 5 * time.Second

var sslModes = map[string]bool{
	"disable":     true,
	"require":     true,
	"verify-ca":   true,
	"verify-full": true,
}

// Config describes one managed PostgreSQL target.
type Config struct {
	Host         string
	Port         int
	DatabaseName string
	Username     string
	Password     string
	SSLMode      string
}

// DSN builds a postgres:// URL with percent-encoded credentials.
func (c Config) DSN() (string, error) {
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
func Ping(ctx context.Context, dsn string) error {
	ctx, cancel := context.WithTimeout(ctx, PingTimeout)
	defer cancel()

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("open target database: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect to target database: %w", err)
	}
	return nil
}
