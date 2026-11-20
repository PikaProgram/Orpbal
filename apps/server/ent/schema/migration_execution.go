package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// MigrationExecution holds one recorded run of a migration against a target.
type MigrationExecution struct {
	ent.Schema
}

func (MigrationExecution) Fields() []ent.Field {
	return []ent.Field{
		field.Int("migration_id").
			Positive().
			Comment("FK to migration; the migration this execution runs"),
		field.Int("connection_id").
			Positive().
			Comment("FK to database_connection; target the SQL ran against"),
		field.Enum("kind").
			Values("up", "rollback", "forward", "retry", "batch_revert"),
		field.Enum("status").
			Values("draft", "pending_review", "approved", "queued", "running", "success", "failed", "partial", "cancelled"),
		field.Text("sql_executed").
			Optional().
			Comment("Copy of the SQL that actually ran; immutable once set"),
		field.Text("sql_down_executed").
			Optional().
			Comment("Copy of the down SQL used for rollback or batch_revert"),
		field.Text("error").
			Optional(),
		field.Int("reverted_execution_id").
			Optional().
			Nillable().
			Comment("Execution cancelled by this execution's rollback"),
		field.Int("actor_id").
			Positive().
			Comment("FK to platform_user; the user who triggered the execution"),
		field.Int64("batch_cursor").
			Optional().
			Nillable().
			Comment("Last committed cursor upper bound"),
		field.Int("batches_done").
			Default(0),
		field.Int64("rows_done").
			Default(0),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("finished_at").
			Optional().
			Nillable(),
	}
}

// Edges points to the migration, actor, reverted execution and snapshots.
func (MigrationExecution) Edges() []ent.Edge {
	return []ent.Edge{
		edge.
			From("migration", Migration.Type).
			Field("migration_id").
			Ref("executions").
			Unique().
			Required(),
		edge.
			To("actor", PlatformUser.Type).
			Field("actor_id").
			Unique().
			Required(),
		edge.
			To("reverted", MigrationExecution.Type).
			Field("reverted_execution_id").
			Unique(),
		edge.
			To("jobs", MigrationJob.Type),
		edge.
			To("snapshots", SchemaSnapshot.Type),
	}
}

// Indexes keeps an execution's history queryable in order.
func (MigrationExecution) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("migration_id", "created_at"),
	}
}
