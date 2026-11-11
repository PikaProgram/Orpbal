package target

import (
	"os"
	"testing"

	"Orpbal/internal/api"
)

// Throwaway smoke: run against a disposable MySQL 8, env MYSQL_SMOKE_DSN set
// to the mysql:// DSN. Exercises driver open, version floor, allowlist,
// introspection, counts, mutations, and MySQL explain.
func TestMySQLSmoke(t *testing.T) {
	stored := os.Getenv("MYSQL_SMOKE_DSN")
	if stored == "" {
		t.Skip("MYSQL_SMOKE_DSN not set")
	}

	m := NewManager(nil, nil)
	tg, err := m.Target(stored)
	if err != nil {
		t.Fatalf("open target: %v", err)
	}

	// Clean slate: the smoke data is throwaway. smoke_r first: it has an FK to smoke_t.
	for _, drop := range []string{"smoke_r", "smoke_t"} {
		if err := tg.Execute(t.Context(), "DROP TABLE IF EXISTS "+drop); err != nil {
			t.Fatalf("drop %s: %v", drop, err)
		}
	}

	// Version floor: an allowed read passes through pool open + CheckMySQL8.
	cols, rows, err := tg.Query(t.Context(), "SELECT 1 AS one", 10)
	if err != nil {
		t.Fatalf("Query select 1: %v", err)
	}
	if got := cols[0]; got != "one" || len(rows) != 1 {
		t.Fatalf("unexpected result %v %v", cols, rows)
	}

	if _, _, err := tg.Query(t.Context(), "INSERT INTO t VALUES (1)", 10); err == nil {
		t.Fatal("allowlist accepted INSERT")
	}

	if _, _, err := tg.Query(t.Context(), "SELECT 1; DELETE FROM t", 10); err == nil {
		t.Fatal("allowlist accepted multi-statement DELETE")
	}

	if err := tg.Execute(t.Context(), "CREATE TABLE smoke_t (a INT NOT NULL PRIMARY KEY, b VARCHAR(10))"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if err := tg.Execute(t.Context(), "CREATE TABLE smoke_r (r INT NOT NULL, s INT NOT NULL, PRIMARY KEY (r), FOREIGN KEY (s) REFERENCES smoke_t(a))"); err != nil {
		t.Fatalf("create fk table: %v", err)
	}
	if err := tg.Execute(t.Context(), "CREATE INDEX smoke_b ON smoke_t (b)"); err != nil {
		t.Fatalf("create index: %v", err)
	}

	schema, err := tg.IntrospectSchema(t.Context())
	if err != nil {
		t.Fatalf("introspect schema: %v", err)
	}
	if len(schema.Schemas) != 1 || schema.Schemas[0].Schema != "smoketest" {
		t.Fatalf("schema grouping wrong: %+v", schema.Schemas)
	}
	var totalColumns int
	var tb *api.SchemaTable
	for i := range schema.Schemas[0].Tables {
		totalColumns += len(schema.Schemas[0].Tables[i].Columns)
		if schema.Schemas[0].Tables[i].Name == "smoke_t" {
			tb = &schema.Schemas[0].Tables[i]
		}
	}
	if totalColumns != 4 { // smoke_r.r, smoke_r.s, smoke_t.a, smoke_t.b
		t.Fatalf("total columns = %d", totalColumns)
	}
	if tb == nil {
		t.Fatalf("smoke_t missing: %+v", schema.Schemas[0].Tables)
	}
	if len(tb.Columns) != 2 {
		t.Fatalf("columns on smoke_t: %+v", tb.Columns)
	}
	c0 := tb.Columns[0]
	if c0.Name != "a" || c0.OrdinalPosition != 1 || c0.IsNullable || c0.ColumnDefault != nil {
		t.Fatalf("column a wrong: %+v", c0)
	}
	var fkSeen, pkSeen bool
	for _, s := range schema.Schemas {
		for _, tbl := range s.Tables {
			if tbl.Constraints == nil {
				continue
			}
			for _, c := range *tbl.Constraints {
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
	idxSeen := false
	for _, s := range schema.Schemas {
		for _, tbl := range s.Tables {
			if tbl.Indexes == nil {
				continue
			}
			for _, idx := range *tbl.Indexes {
				if idx.Name == "smoke_b" {
					idxSeen = true
				}
			}
		}
	}
	if !idxSeen {
		t.Fatalf("index smoke_b missing from introspected schema")
	}

	counts, err := tg.TableCounts(t.Context())
	if err != nil {
		t.Fatalf("table counts: %v", err)
	}
	countSeen := false
	for _, c := range counts {
		if c.Table == "smoke_t" && c.Columns == 2 {
			countSeen = true
		}
	}
	if !countSeen {
		t.Fatalf("table counts missing smoke_t: %+v", counts)
	}

	ins, err := tg.Mutate(t.Context(), api.MutateRowsRequest{Op: api.Insert, Schema: "smoketest", Table: "smoke_t", Values: &map[string]any{"a": 1, "b": "x"}})
	if err != nil || ins != 1 {
		t.Fatalf("insert: affected=%d err=%v", ins, err)
	}
	ups, err := tg.Mutate(t.Context(), api.MutateRowsRequest{Op: api.Update, Pk: &map[string]any{"a": 1}, Schema: "smoketest", Table: "smoke_t", Values: &map[string]any{"b": "y"}})
	if err != nil || ups != 1 {
		t.Fatalf("update: affected=%d err=%v", ups, err)
	}
	dels, err := tg.Mutate(t.Context(), api.MutateRowsRequest{Op: api.Delete, Pk: &map[string]any{"a": 1}, Schema: "smoketest", Table: "smoke_t"})
	if err != nil || dels != 1 {
		t.Fatalf("delete: affected=%d err=%v", dels, err)
	}
	_, countRows, err := tg.Query(t.Context(), "SELECT COUNT(*) FROM smoke_t", 10)
	if err != nil {
		t.Fatalf("count query: %v", err)
	}
	if len(countRows) != 1 {
		t.Fatalf("count rows = %d: %v", len(countRows), countRows)
	}

	plan, err := tg.Explain(t.Context(), "SELECT a, b FROM smoke_t WHERE b = 'x'", false)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	if plan.Engine != "mysql" || len(plan.Columns) == 0 || len(plan.Rows) == 0 {
		t.Fatalf("unexpected mysql explain: %+v", plan)
	}
	analyzed, err := tg.Explain(t.Context(), "SELECT COUNT(*) FROM smoke_t", true)
	if err != nil {
		t.Fatalf("explain analyze: %v", err)
	}
	if analyzed.Engine != "mysql" || len(analyzed.Rows) == 0 {
		t.Fatalf("unexpected mysql explain analyze: %+v", analyzed)
	}
	if _, err := tg.Explain(t.Context(), "EXPLAIN SELECT 1", false); err == nil {
		t.Fatal("explain accepted a pre-prefixed statement")
	}
}
