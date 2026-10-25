package handler

import (
	"testing"
)

// rows are driver-returned [][]any; buildSchema must fold them into nested
// schema > table structures without dropping rows (regression: findTable
// appended to a slice copy, so newly discovered tables vanished).
func TestBuildSchemaFoldsRows(t *testing.T) {
	colRows := [][]any{
		{"public", "users", "id", "integer", "NO", int64(1), nil, nil},
		{"public", "users", "email", "text", "YES", int64(2), "gen_random_uuid()", nil},
		{"public", "users", "sessions", "character varying", "YES", int64(3), nil, int64(64)},
		{"app", "orders", "id", "bigint", "NO", int64(1), nil, nil},
	}
	constraintRows := [][]any{
		// schema, table, type, name, column, referenced table, referenced columns
		{"public", "users", "PRIMARY KEY", "users_pkey", "id", "", ""},
		{"public", "users", "FOREIGN KEY", "orders_user_fk", "id", "orders", "user_id"},
	}
	indexRows := [][]any{
		{"public", "users", "users_pkey", "CREATE UNIQUE INDEX users_pkey ON public.users USING btree (id)"},
		{"public", "users", "users_email_idx", "CREATE INDEX users_email_idx ON public.users (email)"},
	}

	schema := buildSchema(
		[]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "column_default", "character_maximum_length"},
		colRows, constraintRows, indexRows,
	)

	if len(schema.Schemas) != 2 {
		t.Fatalf("schemas = %d, want 2 (public, app)", len(schema.Schemas))
	}
	public := schema.Schemas[0]
	if public.Schema != "public" || len(public.Tables) != 1 {
		t.Fatalf("public schema tables = %+v, want one users table", public.Tables)
	}
	users := public.Tables[0]
	if len(users.Columns) != 3 {
		t.Fatalf("users columns = %d, want 3", len(users.Columns))
	}
	if users.Columns[0].Name != "id" || users.Columns[0].IsNullable {
		t.Fatalf("first column = %+v, want non-nullable id", users.Columns[0])
	}
	if users.Columns[0].OrdinalPosition != 1 || users.Columns[1].OrdinalPosition != 2 {
		t.Fatalf("ordinal positions = %+v, want 1,2", users.Columns[0:2])
	}
	if users.Columns[1].ColumnDefault == nil || *users.Columns[1].ColumnDefault != "gen_random_uuid()" {
		t.Fatalf("email default = %+v, want gen_random_uuid()", users.Columns[1].ColumnDefault)
	}
	if users.Columns[2].CharacterMaximumLength == nil || *users.Columns[2].CharacterMaximumLength != 64 {
		t.Fatalf("sessions max length = %+v, want 64", users.Columns[2].CharacterMaximumLength)
	}
	if users.Columns[1].Name != "email" || !users.Columns[1].IsNullable {
		t.Fatalf("second column = %+v, want nullable email", users.Columns[1])
	}
	if users.Constraints == nil || len(*users.Constraints) != 2 {
		t.Fatalf("users constraints = %+v, want 2", users.Constraints)
	}
	pk := (*users.Constraints)[0]
	if pk.Type != "PRIMARY KEY" || pk.Columns[0] != "id" {
		t.Fatalf("pk constraint = %+v", pk)
	}
	fk := (*users.Constraints)[1]
	if fk.ReferencedTable == nil || *fk.ReferencedTable != "orders" {
		t.Fatalf("fk referenced table = %+v, want orders", fk.ReferencedTable)
	}
	if users.Indexes == nil || len(*users.Indexes) != 2 {
		t.Fatalf("users indexes = %+v, want 2", users.Indexes)
	}
	if !(*users.Indexes)[0].IsUnique || (*users.Indexes)[1].IsUnique {
		t.Fatalf("index uniqueness flags = %+v", users.Indexes)
	}

	app := schema.Schemas[1]
	if app.Schema != "app" || len(app.Tables) != 1 || app.Tables[0].Name != "orders" {
		t.Fatalf("app schema = %+v, want one orders table", app)
	}
	// Schemas with no rows in the constraint/index queries still keep their tables.
	if app.Tables[0].Constraints != nil || app.Tables[0].Indexes != nil {
		t.Fatalf("orders optional lists = %+v, want nil", app.Tables[0])
	}
}

func TestParseIndexColumns(t *testing.T) {
	cases := map[string][]string{
		"CREATE UNIQUE INDEX users_pkey ON public.users USING btree (id)":      {"id"},
		"CREATE INDEX idx ON public.t (a, b DESC)":                             {"a", "b DESC"},
		"CREATE INDEX idx ON schema.t USING gin (lower(name), left(email, 3))": {"lower(name)", "left(email, 3)"},
		"CREATE INDEX idx ON public.t (col) WHERE (status > 0)":                {"col"},
		"CREATE INDEX idx ON public.t (a, 'b, c')":                             {"a", "'b, c'"},
		"CREATE INDEX idx ON t USING hash (('symmetric'::text))":               {"('symmetric'::text)"},
	}
	for def, want := range cases {
		got := parseIndexColumns(def)
		if len(got) != len(want) {
			t.Errorf("parseIndexColumns(%q) = %q, want %q", def, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("parseIndexColumns(%q) = %q, want %q", def, got, want)
				break
			}
		}
	}
}
