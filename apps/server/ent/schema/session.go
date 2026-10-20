package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Session holds the schema definition for the Session entity.
type Session struct {
	ent.Schema
}

// Fields of the Session.
func (Session) Fields() []ent.Field {
	return []ent.Field{
		field.Int("user_id"),
		field.String("token_hash").
			NotEmpty().
			Unique(),
		field.String("previous_token_hash").
			Optional(),
		field.Time("previous_valid_until").
			Optional(),
		field.String("csrf_hash").
			NotEmpty().
			Sensitive(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("last_seen_at").
			Default(time.Now),
		field.Time("rotated_at").
			Default(time.Now),
		field.Time("idle_expires_at"),
		field.Time("absolute_expires_at"),
	}
}

// Edges of the Session.
func (Session) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", PlatformUser.Type).
			Ref("sessions").
			Field("user_id").
			Unique().
			Required(),
	}
}

// Indexes of the Session.
func (Session) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id"),
		index.Fields("previous_token_hash"),
	}
}
