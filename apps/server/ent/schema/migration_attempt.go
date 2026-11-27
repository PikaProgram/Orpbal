package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// MigrationAttempt is the append-only record of ONE run (attempt) of an
// execution: every worker claim writes exactly one row and terminal states
// are written once — rows are never reset or overwritten afterwards. This is
// the per-attempt layer of migration history required by the M2 spec
// (migration history records every attempt, successful or failed, and must
// not be overwritten). migration_executions keeps mirroring the live status
// of the latest attempt for list views.
type MigrationAttempt struct {
	ent.Schema
}

func (MigrationAttempt) Fields() []ent.Field {
	return []ent.Field{
		field.Int("migration_id").
			Positive().
			Comment("FK to migration; the migration this attempt runs"),
		field.Int("execution_id").
			Positive().
			Comment("FK to migration_execution; the approved execution being attempted"),
		field.Int("connection_id").
			Positive().
			Comment("FK to database_connection; target the SQL ran against"),
		field.Int("attempt").
			Default(1).
			Comment("1-based attempt number within the execution"),
		field.Enum("kind").
			Values("up", "rollback", "forward", "retry", "batch_revert"),
		field.Enum("status").
			Values("running", "success", "failed", "partial", "cancelled"),
		field.Text("sql_executed").
			Optional().
			Comment("Copy of the SQL that actually ran in this attempt; immutable once set"),
		field.Text("error").
			Optional().
			Comment("Failure reason (with SQLSTATE when available) for failed attempts"),
		field.Int("actor_id").
			Positive().
			Comment("FK to platform_user; the user who submitted the execution"),
		field.String("worker_id").
			Optional().
			Comment("Worker instance that claimed this attempt"),
		field.Int64("batch_cursor").
			Optional().
			Nillable().
			Comment("Last committed cursor upper bound at attempt end"),
		field.Int("batches_done").
			Default(0),
		field.Int64("rows_done").
			Default(0),
		field.Time("started_at").
			Default(time.Now).
			Immutable(),
		field.Time("finished_at").
			Optional().
			Nillable(),
	}
}

// Edges points to the execution, migration and actor.
func (MigrationAttempt) Edges() []ent.Edge {
	return []ent.Edge{
		edge.
			From("execution", MigrationExecution.Type).
			Field("execution_id").
			Ref("attempts").
			Unique().
			Required(),
		edge.
			From("migration", Migration.Type).
			Field("migration_id").
			Ref("attempts").
			Unique().
			Required(),
		edge.
			To("actor", PlatformUser.Type).
			Field("actor_id").
			Unique().
			Required(),
	}
}

// Indexes keeps the append-only history ordered and unique per attempt.
func (MigrationAttempt) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("execution_id", "attempt").Unique(),
		index.Fields("migration_id", "started_at"),
	}
}
