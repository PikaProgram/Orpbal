package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"Orpbal/ent"
	"Orpbal/ent/migrate"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	_ "github.com/lib/pq"
)

var client *ent.Client
var rawDB *sql.DB

// InitDB opens the PlatformDatabase and applies the non-destructive Ent schema.
// DATABASE_URL is required: the platform does not fall back to SQLite. One
// shared *sql.DB backs both the ent client and direct-SQL consumers (queue
// gauges), so pool settings stay consistent.
func InitDB(ctx context.Context) (*ent.Client, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	drv := entsql.OpenDB(dialect.Postgres, db)
	c := ent.NewClient(ent.Driver(drv))
	if err := c.Schema.Create(ctx, migrate.WithDropColumn(false), migrate.WithDropIndex(false)); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}

	client = c
	rawDB = db
	return client, nil
}

// SQLDB returns the raw platform pool (metrics gauge refresh and other
// direct-SQL consumers). Nil before InitDB.
func SQLDB() *sql.DB { return rawDB }

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
