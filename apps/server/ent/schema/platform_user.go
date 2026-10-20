package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// PlatformUser holds the schema definition for the PlatformUser entity.
type PlatformUser struct {
	ent.Schema
}

// Fields of the PlatformUser.
func (PlatformUser) Fields() []ent.Field {
	return []ent.Field{
		field.String("username").
			NotEmpty().
			Unique().
			Immutable(),
		field.String("password_hash").
			NotEmpty().
			Sensitive(),
		field.Bool("is_platform_admin").
			Default(false),
		field.Bool("is_active").
			Default(true),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Edges of the PlatformUser.
func (PlatformUser) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("memberships", GroupMembership.Type),
		edge.To("sessions", Session.Type),
	}
}
