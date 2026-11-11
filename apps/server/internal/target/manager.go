package target

import (
	"context"
	"database/sql"
	"sync"

	"Orpbal/ent"
	"Orpbal/ent/databaseconnection"
	"Orpbal/internal/security/crypto"
	"Orpbal/internal/security/dsn"
)

// Manager keeps one connection pool per stored DSN and opens managed targets.
type Manager struct {
	open   func(stored string) (*sql.DB, error)
	cipher *crypto.Cipher
	pools  sync.Map // stored dsn -> *sql.DB
}

// NewManager builds a Manager; open defaults to defaultOpen, cipher may be
// nil only when Open/InvalidateConnection are never called (smoke tests).
func NewManager(open func(stored string) (*sql.DB, error), cipher *crypto.Cipher) *Manager {
	if open == nil {
		open = defaultOpen
	}
	return &Manager{open: open, cipher: cipher}
}

// pool returns the cached connection for dsn, opening and caching it on first use.
func (m *Manager) pool(dsn string) (*sql.DB, error) {
	if db, ok := m.pools.Load(dsn); ok {
		return db.(*sql.DB), nil
	}
	db, err := m.open(dsn)
	if err != nil {
		return nil, err
	}
	actual, loaded := m.pools.LoadOrStore(dsn, db)
	if loaded {
		db.Close()
	}
	return actual.(*sql.DB), nil
}

// Invalidate closes and forgets the cached pool for dsn so the next use
// reconnects with the connection's current settings.
func (m *Manager) Invalidate(dsn string) {
	if db, ok := m.pools.LoadAndDelete(dsn); ok {
		if closer, ok := db.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}
}

// Open resolves one managed connection: ent lookup (enabled only), password
// decrypt, dsn.Config assembly, pool open, dialect selection.
func (m *Manager) Open(ctx context.Context, client *ent.Client, id int) (*Target, error) {
	connection, err := client.DatabaseConnection.Query().
		Where(databaseconnection.ID(id), databaseconnection.IsEnabled(true)).
		Only(ctx)
	if err != nil {
		return nil, err
	}
	stored, err := dsnFor(connection, m.cipher)
	if err != nil {
		return nil, err
	}
	return m.Target(stored)
}

// Target resolves one stored DSN directly (pool + dialect selection).
// The seam used by module-level smoke tests.
func (m *Manager) Target(stored string) (*Target, error) {
	db, err := m.pool(stored)
	if err != nil {
		return nil, err
	}
	dialect := NewPostgresDialect()
	if isMySQL(stored) {
		dialect = NewMySQLDialect()
	}
	return &Target{db: db, dialect: dialect}, nil
}

// InvalidateConnection best-effort invalidates the pool of one stored connection.
func (m *Manager) InvalidateConnection(ctx context.Context, client *ent.Client, id int) {
	connection, err := client.DatabaseConnection.Get(ctx, id)
	if err != nil {
		return
	}
	stored, err := dsnFor(connection, m.cipher)
	if err != nil {
		return
	}
	m.Invalidate(stored)
}

func dsnFor(connection *ent.DatabaseConnection, cipher *crypto.Cipher) (string, error) {
	password := ""
	if len(connection.PasswordCiphertext) > 0 {
		plain, err := cipher.Decrypt(connection.PasswordCiphertext)
		if err != nil {
			return "", err
		}
		password = string(plain)
	}
	config := dsn.Config{
		Engine:       string(connection.Engine),
		Host:         connection.Host,
		Port:         connection.Port,
		DatabaseName: connection.DatabaseName,
		Username:     connection.Username,
		Password:     password,
		SSLMode:      string(connection.SslMode),
	}
	return config.DSN()
}
