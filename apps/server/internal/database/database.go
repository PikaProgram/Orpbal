package database

import (
	"context"
	"fmt"
	"os"

	"Orpbal/ent"
	"Orpbal/ent/migrate"

	_ "github.com/lib/pq"
)

var client *ent.Client

// InitDB opens the PlatformDatabase and applies the non-destructive Ent schema.
// DATABASE_URL is required: the platform does not fall back to SQLite.
func InitDB(ctx context.Context) (*ent.Client, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	c, err := ent.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := c.Schema.Create(ctx, migrate.WithDropColumn(false), migrate.WithDropIndex(false)); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}

	client = c
	return client, nil
}

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
