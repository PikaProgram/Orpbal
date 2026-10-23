package target

import "database/sql"

func defaultOpen(dsn string) (*sql.DB, error) {
	return sql.Open("postgres", dsn)
}
