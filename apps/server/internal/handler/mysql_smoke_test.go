package handler

import (
	"os"
	"strings"
	"testing"

	"Orpbal/internal/api"
	"Orpbal/internal/target"
)

// Throwaway smoke: run against a disposable MySQL 8, env MYSQL_SMOKE_DSN set
// to the mysql:// DSN. Exercises driver open, version floor, allowlist, and
// the MySQL introspection SQL projections.
func TestMySQLSmoke(t *testing.T) {
	stored := os.Getenv("MYSQL_SMOKE_DSN")
	if stored == "" {
		t.Skip("MYSQL_SMOKE_DSN not set")
	}
	v := target.NewViewer(nil)

	// Clean slate: the smoke data is throwaway.
	for _, drop := range []string{"smoke_r", "smoke_t"} {
		_ = v.Execute(t.Context(), stored, "DROP TABLE IF EXISTS "+drop)
	}

	// Version floor: an allowed read passes through pool open + checkMySQL8.
	cols, rows, err := v.Query(t.Context(), stored, "SELECT 1 AS one", 10)
	if err != nil {
		t.Fatalf("Query select 1: %v", err)
	}
	if got := cols[0]; got != "one" || len(rows) != 1 {
		t.Fatalf("unexpected result %v %v", cols, rows)
	}

	if _, _, err := v.Query(t.Context(), stored, "INSERT INTO t VALUES (1)", 10); err == nil {
		t.Fatal("allowlist accepted INSERT")
	}

	if _, _, err := v.Query(t.Context(), stored, "SELECT 1; DELETE FROM t", 10); err == nil {
		t.Fatal("allowlist accepted multi-statement DELETE")
	}

	if err := v.Execute(t.Context(), stored, "CREATE TABLE smoke_t (a INT NOT NULL PRIMARY KEY, b VARCHAR(10))"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if err := v.Execute(t.Context(), stored, "CREATE TABLE smoke_r (r INT NOT NULL, s INT NOT NULL, PRIMARY KEY (r), FOREIGN KEY (s) REFERENCES smoke_t(a))"); err != nil {
		t.Fatalf("create fk table: %v", err)
	}
	if err := v.Execute(t.Context(), stored, "CREATE INDEX smoke_b ON smoke_t (b)"); err != nil {
		t.Fatalf("create index: %v", err)
	}

	_, colRows, err := v.Query(t.Context(), stored, mysqlColumnsSQL, 0)
	if err != nil {
		t.Fatalf("columns introspection: %v", err)
	}
	if len(colRows) != 4 { // smoke_r.r, smoke_r.s, smoke_t.a, smoke_t.b
		t.Fatalf("column rows = %d: %v", len(colRows), colRows)
	}
	schema := buildSchema(nil, colRows, nil, nil)
	if len(schema.Schemas) != 1 || schema.Schemas[0].Schema != "smoketest" {
		t.Fatalf("schema grouping wrong: %+v", schema.Schemas)
	}
	tables := schema.Schemas[0].Tables
	var tb *api.SchemaTable
	for i := range tables {
		if tables[i].Name == "smoke_t" {
			tb = &tables[i]
		}
	}
	if tb == nil {
		t.Fatalf("smoke_t missing: %+v", tables)
	}
	if len(tb.Columns) != 2 {
		t.Fatalf("columns on first table: %+v", tb.Columns)
	}
	c0 := tb.Columns[0]
	if c0.Name != "a" || c0.OrdinalPosition != 1 || c0.IsNullable || c0.ColumnDefault != nil {
		t.Fatalf("column a wrong: %+v", c0)
	}

	_, conRows, err := v.Query(t.Context(), stored, mysqlConstraintsSQL, 0)
	if err != nil {
		t.Fatalf("constraints introspection: %v", err)
	}
	full := buildSchema(nil, colRows, conRows, nil)
	var fkSeen, pkSeen bool
	for _, s := range full.Schemas {
		for _, tb := range s.Tables {
			if tb.Constraints == nil {
				continue
			}
			for _, c := range *tb.Constraints {
				if c.Type == "FOREIGN KEY" && c.ReferencedTable != nil && *c.ReferencedTable == "smoke_t" {
					fkSeen = true
				}
				if c.Type == "PRIMARY KEY" {
					pkSeen = true
				}
			}
		}
	}
	if !fkSeen || !pkSeen {
		t.Fatalf("constraints missing fk=%v pk=%v", fkSeen, pkSeen)
	}

	_, idxRows, err := v.Query(t.Context(), stored, mysqlIndexesSQL, 0)
	if err != nil {
		t.Fatalf("indexes introspection: %v", err)
	}
	found := false
	for _, r := range idxRows {
		def := strings.TrimSpace(toString(r[3]))
		if strings.HasPrefix(def, "CREATE INDEX smoke_b ON smoke_t (b") {
			found = true
		}
	}
	if !found {
		t.Fatalf("synthesized indexdef missing: %v", idxRows)
	}
}
