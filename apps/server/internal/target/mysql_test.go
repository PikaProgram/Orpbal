package target

import (
	"errors"
	"reflect"
	"testing"

	"Orpbal/internal/api"
)

func TestMySQLMutationBuilders(t *testing.T) {
	mutation := NewMySQLDialect()

	tests := []struct {
		name    string
		body    api.MutateRowsRequest
		wantSQL string
		wantArg []any
	}{
		{
			name: "insert",
			body: api.MutateRowsRequest{
				Op:     api.Insert,
				Schema: "smoketest",
				Table:  "smoke_t",
				Values: &map[string]any{"b": "x", "a": 1},
			},
			wantSQL: "INSERT INTO `smoketest`.`smoke_t` (`a`, `b`) VALUES (?, ?)",
			wantArg: []any{1, "x"},
		},
		{
			name: "update",
			body: api.MutateRowsRequest{
				Op:     api.Update,
				Schema: "s",
				Table:  "t",
				Pk:     &map[string]any{"id": 7},
				Values: &map[string]any{"b": "y"},
			},
			wantSQL: "UPDATE `s`.`t` SET `b` = ? WHERE `id` = ?",
			wantArg: []any{"y", 7},
		},
		{
			name: "delete",
			body: api.MutateRowsRequest{
				Op:     api.Delete,
				Schema: "s",
				Table:  "t",
				Pk:     &map[string]any{"id": 7},
			},
			wantSQL: "DELETE FROM `s`.`t` WHERE `id` = ?",
			wantArg: []any{7},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotSQL, gotArgs, err := mutation.Mutation(tc.body)
			if err != nil {
				t.Fatalf("Mutation() error = %v, want nil", err)
			}
			if gotSQL != tc.wantSQL {
				t.Errorf("Mutation() SQL = %q, want %q", gotSQL, tc.wantSQL)
			}
			if !reflect.DeepEqual(gotArgs, tc.wantArg) {
				t.Errorf("Mutation() args = %v, want %v", gotArgs, tc.wantArg)
			}
		})
	}

	failures := []struct {
		name string
		body api.MutateRowsRequest
	}{
		{
			name: "insert without values",
			body: api.MutateRowsRequest{
				Op:     api.Insert,
				Schema: "s",
				Table:  "t",
			},
		},
		{
			name: "update without pk",
			body: api.MutateRowsRequest{
				Op:     api.Update,
				Schema: "s",
				Table:  "t",
				Values: &map[string]any{"a": 1},
			},
		},
		{
			name: "delete without pk",
			body: api.MutateRowsRequest{
				Op:     api.Delete,
				Schema: "s",
				Table:  "t",
			},
		},
		{
			name: "unknown op",
			body: api.MutateRowsRequest{
				Op:     api.MutateRowsRequestOp("bogus"),
				Schema: "s",
				Table:  "t",
			},
		},
		{
			name: "bad table identifier",
			body: api.MutateRowsRequest{
				Op:     api.Insert,
				Schema: "s",
				Table:  "a; DROP",
				Values: &map[string]any{"a": 1},
			},
		},
	}
	for _, tc := range failures {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := mutation.Mutation(tc.body)
			if err == nil {
				t.Fatal("Mutation() error = nil, want ErrInvalidMutation")
			}
			if !errors.Is(err, ErrInvalidMutation) {
				t.Errorf("Mutation() error = %v, want it to wrap ErrInvalidMutation", err)
			}
		})
	}
}

func TestMySQLQuoteIdent(t *testing.T) {
	mutation := NewMySQLDialect()

	got, err := mutation.QuoteIdent("a_b")
	if err != nil {
		t.Fatalf("QuoteIdent(\"a_b\") error = %v, want nil", err)
	}
	if got != "`a_b`" {
		t.Errorf("QuoteIdent(\"a_b\") = %q, want %q", got, "`a_b`")
	}

	for _, bad := range []string{"a-b", "a b", ""} {
		if _, err := mutation.QuoteIdent(bad); err == nil {
			t.Errorf("QuoteIdent(%q) error = nil, want invalid identifier", bad)
		}
	}
}
