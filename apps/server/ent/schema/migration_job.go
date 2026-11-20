package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// MigrationJob is the async queue row a worker claims to run an execution.
type MigrationJob struct {
	ent.Schema
}

func (MigrationJob) Fields() []ent.Field {
	return []ent.Field{
		field.Int("execution_id").
			Positive().
			Comment("FK to migration_execution; the work item"),
		field.Int("connection_id").
			Positive().
			Comment("FK to database_connection; target the worker connects to"),
		field.Enum("status").
			Values("queued", "running", "success", "failed", "cancelled", "cancelling", "partial").
			Default("queued"),
		field.Int("attempts").
			Default(0),
		field.String("traceparent").
			Optional().
			Comment("W3C trace context passed from API to worker"),
		field.String("locked_by").
			Optional().
			Comment("Worker instance ID currently claiming the job"),
		field.Time("locked_at").
			Optional().
			Nillable(),
		field.Time("heartbeat_at").
			Optional().
			Nillable(),
		field.Time("claimed_at").
			Optional().
			Nillable(),
		field.Text("error").
			Optional(),
		field.Int64("batch_cursor").
			Optional().
			Nillable().
			Comment("Batch checkpoint to resume from"),
		field.Int("batches_done").
			Default(0),
		field.Int64("rows_done").
			Default(0),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("started_at").
			Optional().
			Nillable(),
		field.Time("finished_at").
			Optional().
			Nillable(),
	}
}

// Edges points to the execution this job runs.
func (MigrationJob) Edges() []ent.Edge {
	return []ent.Edge{
		edge.
			From("execution", MigrationExecution.Type).
			Field("execution_id").
			Ref("jobs").
			Unique().
			Required(),
	}
}

// Indexes supports queue polling by status in creation order.
func (MigrationJob) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status", "created_at"),
	}
}
