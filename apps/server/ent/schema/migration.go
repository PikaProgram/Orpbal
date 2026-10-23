package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Migration holds the linear migration history for a managed database.
type Migration struct {
	ent.Schema
}

func (Migration) Fields() []ent.Field {
	return []ent.Field{
		field.Int("connection_id").
			Positive().
			Comment("FK to database_connection; migration applies to this target"),
		field.Int("version").
			Positive(),
		field.Text("sql").
			Comment("SQL executed by this migration"),
		field.Int("executor_id").
			Positive().
			Comment("FK to platform_user; the user who created it"),
		field.Time("executed_at").
			Default(time.Now).
			Comment("When the migration was applied"),
		field.Enum("status").
			Values("pending", "success", "failed"),
	}
}

// Edges points to the target connection and executor.
func (Migration) Edges() []ent.Edge {
	return []ent.Edge{
		edge.
			To("connection", DatabaseConnection.Type).
			Field("connection_id").
			Unique().
			Required(),
		edge.
			To("executor", PlatformUser.Type).
			Field("executor_id").
			Unique().
			Required(),
	}
}

// Indexes ensures the linear history stays queryable per database.
func (Migration) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("connection_id", "version").Unique(),
	}
}
