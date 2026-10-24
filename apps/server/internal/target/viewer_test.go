package target

import (
	"context"
	"database/sql"
	"testing"
)

type fakeOpen struct {
	db *sql.DB
}

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
	v := NewViewer(func(string) (*sql.DB, error) { return nil, nil })
	if _, _, err := v.Query(context.Background(), "postgres://x", "  ", 10); err == nil {
		t.Fatal("expected empty statement to be rejected")
	}
}

func TestExecuteRejectsEmpty(t *testing.T) {
	v := NewViewer(func(string) (*sql.DB, error) { return nil, nil })
	if err := v.Execute(context.Background(), "postgres://x", "  "); err == nil {
		t.Fatal("expected empty statement to be rejected")
	}
}

func TestInvalidate(t *testing.T) {
	v := NewViewer(func(string) (*sql.DB, error) { return nil, nil })
	if v.Invalidate("missing-dsn"); false {
		t.Fatal("unreachable")
	}
	closed := false
	v.pools.Store("dsn", closedDB{closed: &closed})
	v.Invalidate("dsn")
	if !closed {
		t.Fatal("Invalidate did not close the cached pool")
	}
	if _, ok := v.pools.Load("dsn"); ok {
		t.Fatal("Invalidate did not forget the cached pool")
	}
}

type closedDB struct {
	*sql.DB
	closed *bool
}

func (c closedDB) Close() error { *c.closed = true; return nil }
