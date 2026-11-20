package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// MigrationApproval records one group's leader decision on a migration.
type MigrationApproval struct {
	ent.Schema
}

func (MigrationApproval) Fields() []ent.Field {
	return []ent.Field{
		field.Int("migration_id").
			Positive().
			Comment("FK to migration; the migration under review"),
		field.Int("group_id").
			Positive().
			Comment("FK to group; the group whose approval this is"),
		field.Int("leader_id").
			Positive().
			Comment("FK to platform_user; leader expected to decide at submit time"),
		field.Enum("status").
			Values("pending", "approved", "rejected").
			Default("pending"),
		field.Int("decided_by_id").
			Optional().
			Nillable().
			Comment("Actual deciding user; may differ from leader_id"),
		field.Text("comment").
			Optional(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("decided_at").
			Optional().
			Nillable(),
	}
}

// Edges points to the migration, group and expected leader.
func (MigrationApproval) Edges() []ent.Edge {
	return []ent.Edge{
		edge.
			From("migration", Migration.Type).
			Field("migration_id").
			Ref("approvals").
			Unique().
			Required(),
		edge.
			From("group", Group.Type).
			Field("group_id").
			Ref("approvals").
			Unique().
			Required(),
		edge.
			From("leader", PlatformUser.Type).
			Field("leader_id").
			Ref("approvals").
			Unique().
			Required(),
	}
}

// Indexes ensures one approval row per group per migration.
func (MigrationApproval) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("migration_id", "group_id").Unique(),
	}
}
