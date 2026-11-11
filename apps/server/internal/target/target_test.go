package target

import (
	"context"
	"database/sql"
	"testing"
)

func TestReadableStatement(t *testing.T) {
	if err := ReadableStatement("   "); err == nil {
		t.Fatal("expected empty statement to be rejected")
	}
	if err := ReadableStatement(" SELECT 1 "); err != nil {
		t.Fatalf("expected valid statement, got %v", err)
	}
}

func TestFormatValue(t *testing.T) {
	if FormatValue(nil) != nil {
		t.Fatal("nil should stay nil")
	}
	if got := FormatValue([]byte("x")); got != "x" {
		t.Fatalf("[]byte should become string, got %v", got)
	}
}

func TestTruncateCell(t *testing.T) {
	if got := TruncateCell("abcdefghij", 5); got != "abcde…" {
		t.Fatalf("expected truncation, got %v", got)
	}
	if got := TruncateCell(42, 5); got != 42 {
		t.Fatalf("non-string should pass through, got %v", got)
	}
}

func TestQueryRejectsEmpty(t *testing.T) {
	tgt := &Target{dialect: NewPostgresDialect()}
	if _, _, err := tgt.Query(context.Background(), "  ", 10); err == nil {
		t.Fatal("expected empty statement to be rejected")
	}
}

func TestExecuteRejectsEmpty(t *testing.T) {
	tgt := &Target{dialect: NewPostgresDialect()}
	if err := tgt.Execute(context.Background(), "  "); err == nil {
		t.Fatal("expected empty statement to be rejected")
	}
}

func TestInvalidate(t *testing.T) {
	m := NewManager(nil, nil)
	closed := false
	m.pools.Store("dsn", closedDB{closed: &closed})
	m.Invalidate("dsn")
	if !closed {
		t.Fatal("Invalidate did not close the cached pool")
	}
	if _, ok := m.pools.Load("dsn"); ok {
		t.Fatal("Invalidate did not forget the cached pool")
	}
}

type closedDB struct {
	*sql.DB
	closed *bool
}

func (c closedDB) Close() error { *c.closed = true; return nil }
