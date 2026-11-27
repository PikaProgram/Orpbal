package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"Orpbal/ent"
	"Orpbal/ent/migrate"

	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/lib/pq"
)

var (
	client *ent.Client
	rawDB  *sql.DB
)

// InitDB opens the PlatformDatabase and applies the non-destructive Ent schema.
// DATABASE_URL is required: the platform does not fall back to SQLite.
func InitDB(ctx context.Context) (*ent.Client, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	// Keep the *sql.DB handle so observability can expose pool stats without
	// opening a second connection pool.
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	c := ent.NewClient(ent.Driver(entsql.OpenDB("postgres", db)))
	if err := c.Schema.Create(ctx, migrate.WithDropColumn(false), migrate.WithDropIndex(false)); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}

	client = c
	rawDB = db
	return client, nil
}

// RawDB returns the pool behind the Ent client for Prometheus DB stats.
func RawDB() *sql.DB { return rawDB }

// GetClient returns the initialized Ent client.
func GetClient() *ent.Client {
	return client
}

// Close closes the PlatformDatabase connection.
func Close() error {
	if client == nil {
		return nil
	}
	return client.Close()
}
