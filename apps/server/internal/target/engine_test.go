package target

import (
	"strings"
	"testing"

	"Orpbal/internal/security/dsn"
)

func TestIsMySQL(t *testing.T) {
	if !isMySQL(dsn.DSNPrefixMySQL + "user@tcp(h)/db") {
		t.Fatal("isMySQL() rejected a MySQL DSN")
	}
	if isMySQL("postgres://u@h:5432/db") {
		t.Fatal("isMySQL() accepted a Postgres DSN")
	}
}

func TestMySQLReadableStatement(t *testing.T) {
	readable := []string{
		"SELECT * FROM t",
		"select 1",
		"  SELECT 1",
		"SHOW TABLES",
		"DESCRIBE t",
		"DESC t",
		"EXPLAIN SELECT * FROM t",
		"WITH x AS (SELECT 1) SELECT * FROM x",
	}
	for _, sql := range readable {
		if err := MySQLReadableStatement(sql); err != nil {
			t.Fatalf("MySQLReadableStatement(%q) rejected a read: %v", sql, err)
		}
	}
	writes := []string{
		"INSERT INTO t VALUES (1)",
		"UPDATE t SET a = 1",
		"DELETE FROM t",
		"DROP TABLE t",
		"CREATE TABLE t (a int)",
		"ALTER TABLE t ADD COLUMN b int",
		"SET GLOBAL x = 1",
		"SET TRANSACTION READ WRITE",
	}
	for _, sql := range writes {
		if err := MySQLReadableStatement(sql); err != nil {
			continue
		}
		t.Fatalf("MySQLReadableStatement(%q) accepted a write", sql)
	}
	if err := MySQLReadableStatement("SET"); err == nil || !strings.Contains(err.Error(), "allowed") {
		t.Fatalf("MySQLReadableStatement(SET) error = %v", err)
	}
	if err := MySQLReadableStatement("   "); err == nil {
		t.Fatal("MySQLReadableStatement accepted empty input")
	}
}
