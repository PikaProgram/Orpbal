package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SchemaSnapshot stores the target's schema state captured around a migration
// execution, so the UI can render what a migration changed.
type SchemaSnapshot struct {
	ent.Schema
}

func (SchemaSnapshot) Fields() []ent.Field {
	return []ent.Field{
		field.Int("migration_id").
			Positive().
			Comment("FK to migration; snapshot pair belongs to one migration"),
		field.Enum("phase").
			Values("before", "after").
			Comment("Which side of the execution this snapshot captures"),
		field.Text("json").
			Comment("Full DatabaseSchema JSON of the target at capture time"),
	}
}

func (SchemaSnapshot) Edges() []ent.Edge {
	return []ent.Edge{
		edge.
			From("migration", Migration.Type).
			Field("migration_id").
			Ref("snapshots").
			Unique().
			Required(),
	}
}

func (SchemaSnapshot) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("migration_id", "phase").Unique(),
	}
}
