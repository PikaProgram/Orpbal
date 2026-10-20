package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// DatabaseGrant holds the schema definition for the DatabaseGrant entity.
type DatabaseGrant struct {
	ent.Schema
}

// Fields of the DatabaseGrant.
func (DatabaseGrant) Fields() []ent.Field {
	return []ent.Field{
		field.Int("group_id"),
		field.Int("connection_id"),
		field.Enum("access_level").
			Values("view_only", "view_edit"),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Edges of the DatabaseGrant.
func (DatabaseGrant) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("group", Group.Type).
			Ref("grants").
			Field("group_id").
			Unique().
			Required(),
		edge.From("connection", DatabaseConnection.Type).
			Ref("grants").
			Field("connection_id").
			Unique().
			Required(),
	}
}

// Indexes of the DatabaseGrant.
func (DatabaseGrant) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("group_id", "connection_id").Unique(),
	}
}
