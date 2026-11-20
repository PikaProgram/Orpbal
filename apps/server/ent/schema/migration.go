package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Migration holds a versioned migration definition for a managed database.
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
		field.Text("sql_up").
			Comment("Forward SQL executed by this migration"),
		field.Text("sql_down").
			Optional().
			Comment("Rollback SQL; empty means the migration is irreversible"),
		field.Bool("irreversible").
			Default(false),
		field.String("irreversible_reason").
			Optional().
			Comment("Why the migration cannot be rolled back"),
		field.Enum("status").
			Values("draft", "pending_review", "approved", "queued", "running", "success", "failed", "partial", "cancelled"),
		field.Int("author_id").
			Positive().
			Comment("FK to platform_user; the user who created it"),
		field.Int("batch_size").
			Optional().
			Positive().
			Comment("Rows per batch for batch migrations"),
		field.String("cursor_column").
			Optional().
			Comment("Column used as the batch cursor"),
		field.String("batch_table").
			Optional().
			Comment("Table the batch cursor walks; scope of the data migration"),
		field.Int64("min_id").
			Optional().
			Nillable(),
		field.Int64("max_id").
			Optional().
			Nillable(),
		field.Text("template").
			Optional().
			Comment("Batch SQL with {{last_id}},{{batch_size}},{{min_id}},{{max_id}} placeholders"),
	}
}

// Edges points to the target connection, author, executions and snapshots.
func (Migration) Edges() []ent.Edge {
	return []ent.Edge{
		edge.
			To("connection", DatabaseConnection.Type).
			Field("connection_id").
			Unique().
			Required(),
		edge.
			To("author", PlatformUser.Type).
			Field("author_id").
			Unique().
			Required(),
		edge.
			To("executions", MigrationExecution.Type),
		edge.
			To("approvals", MigrationApproval.Type),
		edge.
			To("snapshots", SchemaSnapshot.Type),
	}
}

// Indexes ensures one migration per version per database.
func (Migration) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("connection_id", "version").Unique(),
	}
}
