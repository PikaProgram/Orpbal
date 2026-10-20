package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// GroupMembership holds the schema definition for the GroupMembership entity.
type GroupMembership struct {
	ent.Schema
}

// Fields of the GroupMembership.
func (GroupMembership) Fields() []ent.Field {
	return []ent.Field{
		field.Int("user_id"),
		field.Int("group_id"),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
	}
}

// Edges of the GroupMembership.
func (GroupMembership) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", PlatformUser.Type).
			Ref("memberships").
			Field("user_id").
			Unique().
			Required(),
		edge.From("group", Group.Type).
			Ref("memberships").
			Field("group_id").
			Unique().
			Required(),
	}
}

// Indexes of the GroupMembership.
func (GroupMembership) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "group_id").Unique(),
	}
}
