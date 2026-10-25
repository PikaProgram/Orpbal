package target

import (
	"strings"

	"Orpbal/internal/security/dsn"
)

// isMySQL reports whether a stored DSN belongs to the MySQL engine.
func isMySQL(stored string) bool {
	return strings.HasPrefix(stored, dsn.DSNPrefixMySQL)
}
