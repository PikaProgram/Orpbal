package target

import (
	"database/sql"

	"Orpbal/internal/security/dsn"
)

func defaultOpen(stored string) (*sql.DB, error) {
	driver, connect := dsn.DriverForDSN(stored)
	db, err := sql.Open(driver, connect)
	if err != nil {
		return nil, err
	}
	if driver == dsn.EngineMySQL {
		if err := dsn.CheckMySQL8(db); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return db, nil
}
