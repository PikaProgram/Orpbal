package target

import (
	"errors"
	"reflect"
	"testing"

	"Orpbal/internal/api"
)

func TestPostgresMutationBuilders(t *testing.T) {
	mutation := NewPostgresDialect()

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
				Schema: "public",
				Table:  "users",
				Values: &map[string]any{"name": "b", "id": 1},
			},
			wantSQL: `INSERT INTO "public"."users" ("id", "name") VALUES ($1, $2)`,
			wantArg: []any{1, "b"},
		},
		{
			name: "update",
			body: api.MutateRowsRequest{
				Op:     api.Update,
				Schema: "s",
				Table:  "t",
				Pk:     &map[string]any{"id": 7},
				Values: &map[string]any{"a": 1, "b": "x"},
			},
			wantSQL: `UPDATE "s"."t" SET "a" = $1, "b" = $2 WHERE "id" = $3`,
			wantArg: []any{1, "x", 7},
		},
		{
			name: "delete",
			body: api.MutateRowsRequest{
				Op:     api.Delete,
				Schema: "s",
				Table:  "t",
				Pk:     &map[string]any{"id": 7},
			},
			wantSQL: `DELETE FROM "s"."t" WHERE "id" = $1`,
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

func TestPostgresQuoteIdent(t *testing.T) {
	mutation := NewPostgresDialect()

	got, err := mutation.QuoteIdent("a_b")
	if err != nil {
		t.Fatalf("QuoteIdent(\"a_b\") error = %v, want nil", err)
	}
	if got != `"a_b"` {
		t.Errorf("QuoteIdent(\"a_b\") = %q, want %q", got, `"a_b"`)
	}

	for _, bad := range []string{"a-b", "a b", ""} {
		if _, err := mutation.QuoteIdent(bad); err == nil {
			t.Errorf("QuoteIdent(%q) error = nil, want invalid identifier", bad)
		}
	}
}
