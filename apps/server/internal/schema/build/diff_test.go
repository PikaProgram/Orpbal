package build

import (
	"strings"
	"testing"

	"Orpbal/internal/api"
)

func oneTable(schemaName, table string) api.DatabaseSchema {
	return api.DatabaseSchema{Schemas: []api.SchemaTables{{
		Schema: schemaName,
		Tables: []api.SchemaTable{{Name: table}},
	}}}
}

func findChange(changes []api.SchemaChange, objectID string) *api.SchemaChange {
	for i := range changes {
		if changes[i].ObjectId == objectID {
			return &changes[i]
		}
	}
	return nil
}

func TestDiffIdenticalSchemas(t *testing.T) {
	s := api.DatabaseSchema{Schemas: []api.SchemaTables{{
		Schema: "public",
		Tables: []api.SchemaTable{{
			Name:    "users",
			Columns: []api.SchemaColumn{{Name: "id", Type: "integer", IsNullable: false}},
		}},
	}}}
	if changes := Diff(s, s); len(changes) != 0 {
		t.Fatalf("Diff(identical) = %+v, want no changes", changes)
	}
}

func TestDiffTableAddedAndRemoved(t *testing.T) {
	changes := Diff(oneTable("public", "old"), oneTable("public", "new"))
	if len(changes) != 2 {
		t.Fatalf("changes = %+v, want 2", changes)
	}
	added := findChange(changes, "public.new")
	if added == nil || added.ObjectType != "table" || added.Action != "added" {
		t.Fatalf("added = %+v, want table public.new added", added)
	}
	removed := findChange(changes, "public.old")
	if removed == nil || removed.ObjectType != "table" || removed.Action != "removed" {
		t.Fatalf("removed = %+v, want table public.old removed", removed)
	}
}

func TestDiffColumnChanges(t *testing.T) {
	before := api.DatabaseSchema{Schemas: []api.SchemaTables{{
		Schema: "public",
		Tables: []api.SchemaTable{{
			Name: "users",
			Columns: []api.SchemaColumn{
				{Name: "id", Type: "integer", IsNullable: false},
				{Name: "email", Type: "text", IsNullable: true},
				{Name: "legacy", Type: "text", IsNullable: false},
			},
		}},
	}}}
	after := api.DatabaseSchema{Schemas: []api.SchemaTables{{
		Schema: "public",
		Tables: []api.SchemaTable{{
			Name: "users",
			Columns: []api.SchemaColumn{
				{Name: "id", Type: "bigint", IsNullable: false},
				{Name: "email", Type: "text", IsNullable: false},
				{Name: "fresh", Type: "text", IsNullable: true},
			},
		}},
	}}}
	changes := Diff(before, after)
	if len(changes) != 4 {
		t.Fatalf("changes = %+v, want 4", changes)
	}

	id := findChange(changes, "public.users.id")
	if id == nil || id.Action != "changed" || id.Detail == nil || *id.Detail != "integer NOT NULL → bigint NOT NULL" {
		t.Fatalf("id change = %+v, want type change detail", id)
	}
	email := findChange(changes, "public.users.email")
	if email == nil || email.Action != "changed" || email.Detail == nil || *email.Detail != "text NULL → text NOT NULL" {
		t.Fatalf("email change = %+v, want nullability change detail", email)
	}
	if email.Detail != nil && !strings.Contains(*email.Detail, " → ") {
		t.Fatalf("email detail = %q, want ' → ' separator", *email.Detail)
	}
	fresh := findChange(changes, "public.users.fresh")
	if fresh == nil || fresh.Action != "added" || fresh.Detail == nil || *fresh.Detail != "text NULL" {
		t.Fatalf("fresh change = %+v, want added column detail", fresh)
	}
	legacy := findChange(changes, "public.users.legacy")
	if legacy == nil || legacy.Action != "removed" || legacy.Detail == nil || *legacy.Detail != "text NOT NULL" {
		t.Fatalf("legacy change = %+v, want removed column detail", legacy)
	}
}

func TestDiffConstraintAddedIndexRemoved(t *testing.T) {
	before := api.DatabaseSchema{Schemas: []api.SchemaTables{{
		Schema: "public",
		Tables: []api.SchemaTable{{
			Name:    "users",
			Columns: []api.SchemaColumn{{Name: "email", Type: "text", IsNullable: true}},
			Indexes: &[]api.SchemaIndex{{Name: "old_idx", Columns: []string{"email"}}},
		}},
	}}}
	after := api.DatabaseSchema{Schemas: []api.SchemaTables{{
		Schema: "public",
		Tables: []api.SchemaTable{{
			Name:    "users",
			Columns: []api.SchemaColumn{{Name: "email", Type: "text", IsNullable: true}},
			Constraints: &[]api.SchemaConstraint{{
				Type: "UNIQUE", Name: "uq_email", Columns: []string{"email"},
			}},
		}},
	}}}
	changes := Diff(before, after)
	if len(changes) != 2 {
		t.Fatalf("changes = %+v, want 2", changes)
	}
	con := findChange(changes, "public.users.uq_email")
	if con == nil || con.ObjectType != "constraint" || con.Action != "added" ||
		con.Detail == nil || *con.Detail != "UNIQUE uq_email (email)" {
		t.Fatalf("constraint change = %+v, want added UNIQUE uq_email", con)
	}
	idx := findChange(changes, "public.users.old_idx")
	if idx == nil || idx.ObjectType != "index" || idx.Action != "removed" ||
		idx.Detail == nil || *idx.Detail != "old_idx (email)" {
		t.Fatalf("index change = %+v, want removed old_idx", idx)
	}
}

func TestDiffOrdersAddedChangedRemoved(t *testing.T) {
	before := api.DatabaseSchema{Schemas: []api.SchemaTables{{
		Schema: "public",
		Tables: []api.SchemaTable{
			{
				Name:    "beta",
				Columns: []api.SchemaColumn{{Name: "x", Type: "text", IsNullable: true}},
				Indexes: &[]api.SchemaIndex{{Name: "old_idx", Columns: []string{"x"}}},
			},
			{Name: "zeta"},
		},
	}}}
	after := api.DatabaseSchema{Schemas: []api.SchemaTables{{
		Schema: "public",
		Tables: []api.SchemaTable{
			{
				Name:    "alpha",
				Columns: []api.SchemaColumn{{Name: "k", Type: "text", IsNullable: true}},
				Constraints: &[]api.SchemaConstraint{{
					Type: "UNIQUE", Name: "uq_a", Columns: []string{"k"},
				}},
			},
			{
				Name:    "beta",
				Columns: []api.SchemaColumn{{Name: "x", Type: "integer", IsNullable: true}},
			},
		},
	}}}
	changes := Diff(before, after)
	want := []api.SchemaChange{
		{ObjectType: "table", ObjectId: "public.alpha", Action: "added"},
		{ObjectType: "column", ObjectId: "public.alpha.k", Action: "added"},
		{ObjectType: "constraint", ObjectId: "public.alpha.uq_a", Action: "added"},
		{ObjectType: "column", ObjectId: "public.beta.x", Action: "changed"},
		{ObjectType: "index", ObjectId: "public.beta.old_idx", Action: "removed"},
		{ObjectType: "table", ObjectId: "public.zeta", Action: "removed"},
	}
	if len(changes) != len(want) {
		t.Fatalf("changes = %+v, want %d entries", changes, len(want))
	}
	for i := range want {
		if changes[i].ObjectType != want[i].ObjectType ||
			changes[i].ObjectId != want[i].ObjectId ||
			changes[i].Action != want[i].Action {
			t.Fatalf("changes[%d] = %+v, want %+v", i, changes[i], want[i])
		}
	}
}
