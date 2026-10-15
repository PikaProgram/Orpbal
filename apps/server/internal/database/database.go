package database

import (
	"database/sql"
	"os"
	"strings"

	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

var DB *sql.DB

// InitDB initializes the database connection.
// After running `go generate ./ent`, you can switch to using the Ent client directly.
func InitDB() (*sql.DB, error) {
	dsn := os.Getenv("DATABASE_URL")

	var db *sql.DB
	var err error

	if dsn == "" {
		// Default to SQLite for development
		db, err = sql.Open("sqlite3", "file:app.db?cache=shared&_fk=1")
	} else if strings.HasPrefix(dsn, "postgres") {
		db, err = sql.Open("postgres", dsn)
	} else {
		// Assume SQLite file path
		db, err = sql.Open("sqlite3", "file:"+dsn+"?cache=shared&_fk=1")
	}

	if err != nil {
		return nil, err
	}

	// Verify connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}

	// Create tables if they don't exist (SQLite dev mode)
	if dsn == "" || !strings.HasPrefix(dsn, "postgres") {
		if _, err := db.Exec(sqliteSchema); err != nil {
			db.Close()
			return nil, err
		}
	}

	DB = db
	return DB, nil
}

// GetDB returns the database instance
func GetDB() *sql.DB {
	return DB
}

// Close closes the database connection
func Close() {
	if DB != nil {
		DB.Close()
	}
}

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	email TEXT NOT NULL UNIQUE,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS posts (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	title TEXT NOT NULL,
	content TEXT NOT NULL DEFAULT '',
	published BOOLEAN NOT NULL DEFAULT 0,
	author_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`
