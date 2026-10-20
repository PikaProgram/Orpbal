package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// DatabaseConnection holds the schema definition for the DatabaseConnection entity.
type DatabaseConnection struct {
	ent.Schema
}

// Fields of the DatabaseConnection.
func (DatabaseConnection) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			NotEmpty().
			Unique(),
		field.String("host").
			NotEmpty(),
		field.Int("port").
			Default(5432).
			Range(1, 65535),
		field.String("database_name").
			NotEmpty(),
		field.String("username").
			NotEmpty(),
		field.Bytes("password_ciphertext").
			Optional().
			Sensitive(),
		field.Enum("ssl_mode").
			Values("disable", "require", "verify-ca", "verify-full").
			Default("disable"),
		field.Bool("is_enabled").
			Default(true),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Edges of the DatabaseConnection.
func (DatabaseConnection) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("grants", DatabaseGrant.Type),
	}
}
