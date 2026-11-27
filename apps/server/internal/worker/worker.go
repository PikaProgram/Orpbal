// Package worker executes claimed migration jobs against managed target
// databases: non-batch statements run whole, batch migrations advance a
// cursor in per-batch transactions with checkpoints on the platform DB
// (written right AFTER each target-batch commit — cross-DB 2PC is rejected;
// the accepted crash window re-runs one idempotent batch on reclaim).
package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"Orpbal/ent"
	"Orpbal/ent/migration"
	"Orpbal/ent/migrationattempt"
	"Orpbal/ent/migrationexecution"
	"Orpbal/ent/migrationjob"
	"Orpbal/ent/schemasnapshot"
	"Orpbal/internal/metrics"
	"Orpbal/internal/queue"
	"Orpbal/internal/sqllog"
	"Orpbal/internal/target"
	"Orpbal/internal/tracing"

	"github.com/rs/zerolog"
)

// HeartbeatInterval is how often the worker refreshes the job's heartbeat.
const HeartbeatInterval = 5 * time.Second

// claimGrace is the heartbeat age after which another worker may re-claim a
// stuck running job; the worker's own heartbeat cadence must stay well under it.
const claimGrace = 30 * time.Second

// identifier is the shape a batch cursor column must match before it is
// interpolated into the probe SQL (same rule as internal/target dialects).
var identifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// Runner executes one claimed job at a time. Sequential by design: one job
// per worker process; parallelism comes from running more worker processes.
type Runner struct {
	client  *ent.Client
	queue   *queue.Queue
	targets *target.Manager
	logger  zerolog.Logger
}

// NewRunner builds a Runner over the platform ent client and job queue.
func NewRunner(client *ent.Client, q *queue.Queue, targets *target.Manager, logger zerolog.Logger) *Runner {
	return &Runner{client: client, queue: q, targets: targets, logger: logger}
}

// Run claims jobs until ctx is cancelled. nil claim ⇒ sleep poll interval.
func (r *Runner) Run(ctx context.Context, workerID string, pollInterval time.Duration) {
	for {
		if ctx.Err() != nil {
			return
		}
		claimed, err := r.queue.Claim(ctx, workerID, claimGrace)
		if err != nil {
			if ctx.Err() == nil {
				r.logger.Error().Err(err).Str("event", "claim_error").Msg("claim failed")
			}
		} else if claimed != nil {
			r.run(ctx, workerID, claimed)
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(pollInterval):
		}
	}
}

// run executes one job end to end. Advisory lock + heartbeat lease are owned
// by the queue claim; Release returns both no matter how we exit.
func (r *Runner) run(ctx context.Context, workerID string, job *queue.ClaimedJob) {
	defer job.Release()

	ctx, span := tracing.StartSpan(ctx, job.Traceparent, "worker.execute")
	defer span.End()
	log := tracing.TraceLogger(r.logger, ctx).With().
		Str("event", "job_run").
		Int("job_id", job.ID).
		Int("execution_id", job.ExecutionID).
		Int("connection_id", job.ConnectionID).
		Int("attempt", job.Attempts).
		Str("worker_id", workerID).
		Logger()
	if job.Traceparent != "" {
		log = log.With().Str("traceparent", job.Traceparent).Logger()
	}
	start := time.Now()

	exec, err := r.client.MigrationExecution.Query().
		WithMigration().
		Where(migrationexecution.ID(job.ExecutionID)).
		Only(ctx)
	if err != nil {
		log.Error().Err(err).Str("status", "failed").Msg("load execution")
		_, _ = r.queue.FinishFailed(context.Background(), job.ID, workerID, "load execution: "+err.Error())
		return
	}
	mig := exec.Edges.Migration
	if mig == nil {
		log.Error().Str("status", "failed").Msg("execution has no migration edge")
		_, _ = r.queue.FinishFailed(context.Background(), job.ID, workerID, "execution has no migration edge")
		return
	}

	// Append-only attempt history: every claim of this job gets exactly one
	// migration_attempts row (M2 spec — migration history records ALL
	// attempts, successful or failed, and rows are never overwritten). The
	// execution row keeps mirroring the live status for list views.
	attempt, err := r.client.MigrationAttempt.Create().
		SetExecutionID(exec.ID).
		SetMigrationID(mig.ID).
		SetConnectionID(job.ConnectionID).
		SetAttempt(job.Attempts).
		SetKind(migrationattempt.Kind(exec.Kind)).
		SetStatus(migrationattempt.StatusRunning).
		SetActorID(exec.ActorID).
		SetWorkerID(workerID).
		Save(ctx)
	if err != nil {
		log.Error().Err(err).Str("event", "attempt_record_error").Str("status", "failed").Msg("record attempt failed")
		_, _ = r.queue.FinishFailed(context.Background(), job.ID, workerID, "record attempt: "+err.Error())
		return
	}
	// Target-layer SQL audit logs inherit the full correlation set.
	metrics.Attempts.WithLabelValues(string(exec.Kind)).Inc()
	ctx = sqllog.Attach(ctx, log)

	t, err := r.targets.Open(ctx, r.client, job.ConnectionID)
	if err != nil {
		log.Error().Err(err).Str("status", "failed").Msg("open target")
		r.failExecution(ctx, job, workerID, exec, attempt, "open target: "+err.Error())
		return
	}

	stopHeartbeat := make(chan struct{})
	go r.heartbeat(context.Background(), job.ID, workerID, stopHeartbeat)

	kind, status := r.execute(ctx, log, workerID, job, exec, mig, t, attempt)

	close(stopHeartbeat)
	log = log.With().Int64("duration_ms", time.Since(start).Milliseconds()).Logger()

	if status == migrationexecution.StatusRunning {
		// Abandoned mid-run (e.g. checkpoint failure): the job goes stale and
		// is reclaimed as a NEW attempt; this attempt row must not stay
		// "running" forever in the append-only history.
		r.finalizeAttempt(ctx, attempt, migrationattempt.StatusFailed, "", "abandoned before terminal state; job returned to the queue for reclaim", nil)
	}

	switch status {
	case migrationexecution.StatusSuccess:
		log.Info().Str("migration_id", fmt.Sprint(mig.ID)).Str("status", "success").Str("kind", string(kind)).Msg("job finished")
	case migrationexecution.StatusPartial, migrationexecution.StatusCancelled:
		log.Info().Str("migration_id", fmt.Sprint(mig.ID)).Str("status", string(status)).Str("kind", string(kind)).Msg("job stopped")
	default:
		log.Error().Str("migration_id", fmt.Sprint(mig.ID)).Str("status", string(status)).Str("kind", string(kind)).Msg("job failed")
	}
}

// heartbeat refreshes the job lease every HeartbeatInterval until stopped.
func (r *Runner) heartbeat(ctx context.Context, jobID int, workerID string, stop <-chan struct{}) {
	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if err := r.queue.Heartbeat(ctx, jobID, workerID); err != nil {
				// Lease lost (reclaimed/cancelled); nothing to do — the next
				// claim/checkpoint path detects ownership loss.
				_ = err
			}
		}
	}
}

// failExecution marks the execution failed, mirrors migration status, closes
// the attempt row, and finishes the job through the queue (which decides
// retryability).
func (r *Runner) failExecution(ctx context.Context, job *queue.ClaimedJob, workerID string, exec *ent.MigrationExecution, attempt *ent.MigrationAttempt, msg string) {
	_ = exec.Update().SetStatus(migrationexecution.StatusFailed).SetError(msg).SetNillableFinishedAt(now()).Exec(ctx)
	_ = r.client.Migration.Update().Where(migration.ID(exec.MigrationID)).SetStatus(migration.StatusFailed).Exec(ctx)
	r.finalizeAttempt(ctx, attempt, migrationattempt.StatusFailed, "", msg, nil)
	_, _ = r.queue.FinishFailed(ctx, job.ID, workerID, msg)
}

// finalizeAttempt closes the attempt row exactly once with its terminal
// outcome; attempt rows are append-only history and never reset afterwards.
func (r *Runner) finalizeAttempt(ctx context.Context, attempt *ent.MigrationAttempt, status migrationattempt.Status, sqlExecuted, errText string, cursor *int64) {
	if attempt == nil {
		return
	}
	finish := now()
	update := attempt.Update().
		SetStatus(status).
		SetNillableFinishedAt(finish)
	if sqlExecuted != "" {
		update.SetSQLExecuted(sqlExecuted)
	}
	if errText != "" {
		update.SetError(errText)
	}
	if cursor != nil {
		update.SetNillableBatchCursor(cursor)
	}
	if err := update.Exec(ctx); err != nil {
		r.logger.Error().Err(err).Str("event", "attempt_close_error").
			Int("attempt_id", attempt.ID).Msg("finalize attempt failed")
	}
}

func now() *time.Time {
	t := time.Now()
	return &t
}

// execute runs the claimed execution per its kind and drives every terminal
// state transition (execution, migration, attempt, job). Returns the kind and
// the final execution status for logging.
func (r *Runner) execute(ctx context.Context, log zerolog.Logger, workerID string, job *queue.ClaimedJob, exec *ent.MigrationExecution, mig *ent.Migration, t *target.Target, attempt *ent.MigrationAttempt) (migrationexecution.Kind, migrationexecution.Status) {
	switch exec.Kind {
	case migrationexecution.KindUp, migrationexecution.KindForward, migrationexecution.KindRetry:
		// Batch-mode data migrations walk a cursor template instead of one
		// whole statement (spec: mode per batch).
		if mig.BatchSize > 0 && strings.TrimSpace(mig.Template) != "" && strings.TrimSpace(mig.BatchTable) != "" {
			return r.runBatch(ctx, log, workerID, job, exec, mig, t, attempt, mig.Template)
		}
		return r.runNonBatch(ctx, log, workerID, job, exec, mig, t, attempt, mig.SQLUp)
	case migrationexecution.KindRollback:
		// Rollback always runs the migration's ORIGINAL sql_down (immutable
		// per spec); never request-supplied SQL.
		return r.runNonBatch(ctx, log, workerID, job, exec, mig, t, attempt, mig.SQLDown)
	case migrationexecution.KindBatchRevert:
		// Compensation SQL rides on the batch_revert execution itself
		// (request-supplied at revert-approval time, stored in
		// sql_down_executed); fall back to the original sql_down when absent.
		template := exec.SQLDownExecuted
		if strings.TrimSpace(template) == "" {
			template = mig.SQLDown
		}
		return r.runBatch(ctx, log, workerID, job, exec, mig, t, attempt, template)
	default:
		r.failExecution(ctx, job, workerID, exec, attempt, "unsupported execution kind "+string(exec.Kind))
		return exec.Kind, migrationexecution.StatusFailed
	}
}

// runNonBatch executes one whole statement (no batching).
func (r *Runner) runNonBatch(ctx context.Context, log zerolog.Logger, workerID string, job *queue.ClaimedJob, exec *ent.MigrationExecution, mig *ent.Migration, t *target.Target, attempt *ent.MigrationAttempt, statement string) (migrationexecution.Kind, migrationexecution.Status) {
	if strings.TrimSpace(statement) == "" {
		r.failExecution(ctx, job, workerID, exec, attempt, "no SQL to execute")
		return exec.Kind, migrationexecution.StatusFailed
	}
	// Immutable from here on: record exactly what ran (execution row mirrors
	// the live attempt; the attempt row keeps the immutable copy).
	if err := exec.Update().SetSQLExecuted(statement).SetStatus(migrationexecution.StatusRunning).Exec(ctx); err != nil {
		r.failExecution(ctx, job, workerID, exec, attempt, "mark running: "+err.Error())
		return exec.Kind, migrationexecution.StatusFailed
	}
	if attempt != nil {
		_ = attempt.Update().SetSQLExecuted(statement).Exec(ctx)
	}
	log.Info().Str("event", "exec_start").Str("status", "running").
		Str("migration_id", fmt.Sprint(mig.ID)).Str("kind", string(exec.Kind)).
		Str("sql", truncate(statement)).Msg("executing statement")

	if err := r.captureSnapshot(ctx, t, exec.ID, mig.ID, schemasnapshot.PhaseBefore); err != nil {
		r.failExecution(ctx, job, workerID, exec, attempt, "before snapshot: "+err.Error())
		return exec.Kind, migrationexecution.StatusFailed
	}

	start := time.Now()
	if _, err := t.ExecuteArgs(ctx, statement, nil); err != nil {
		log.Error().Err(err).Str("event", "exec_error").Str("sql", truncate(statement)).Msg("statement failed")
		r.failExecution(ctx, job, workerID, exec, attempt, err.Error())
		return exec.Kind, migrationexecution.StatusFailed
	}
	log.Info().Str("event", "exec_commit").Int64("duration_ms", time.Since(start).Milliseconds()).Msg("statement committed")

	// Rollback: the migration leaves the active state; verify the link first.
	if exec.Kind == migrationexecution.KindRollback && exec.RevertedExecutionID == nil {
		r.failExecution(ctx, job, workerID, exec, attempt, "rollback execution missing reverted_execution_id")
		return exec.Kind, migrationexecution.StatusFailed
	}

	if err := r.captureSnapshot(ctx, t, exec.ID, mig.ID, schemasnapshot.PhaseAfter); err != nil {
		log.Warn().Err(err).Str("event", "snapshot_error").Msg("after-snapshot failed; execution still succeeds")
	}

	finish := time.Now()
	if err := exec.Update().
		SetStatus(migrationexecution.StatusSuccess).
		SetNillableFinishedAt(&finish).
		Exec(ctx); err != nil {
		r.failExecution(ctx, job, workerID, exec, attempt, "mark success: "+err.Error())
		return exec.Kind, migrationexecution.StatusFailed
	}
	r.finalizeAttempt(ctx, attempt, migrationattempt.StatusSuccess, statement, "", nil)

	// Migration rows mirror the latest execution status for the list view.
	migStatus := migration.StatusSuccess
	if exec.Kind == migrationexecution.KindRollback {
		migStatus = migration.StatusCancelled
	}
	_ = r.client.Migration.Update().Where(migration.ID(mig.ID)).SetStatus(migStatus).Exec(ctx)

	if err := r.queue.FinishSuccess(ctx, job.ID, workerID); err != nil {
		log.Error().Err(err).Str("event", "finish_error").Msg("job finish failed; execution stays success")
	}
	return exec.Kind, migrationexecution.StatusSuccess
}

// runBatch advances the cursor template in per-batch transactions. Each batch
// is one tx inside ExecuteArgs; the platform checkpoint is written right
// AFTER the batch commit (no cross-DB 2PC; a crash between the two re-runs
// one idempotent batch on reclaim — documented contract deviation).
func (r *Runner) runBatch(ctx context.Context, log zerolog.Logger, workerID string, job *queue.ClaimedJob, exec *ent.MigrationExecution, mig *ent.Migration, t *target.Target, attempt *ent.MigrationAttempt, template string) (migrationexecution.Kind, migrationexecution.Status) {
	if strings.TrimSpace(template) == "" || mig.BatchSize <= 0 || !identifier.MatchString(mig.CursorColumn) {
		r.failExecution(ctx, job, workerID, exec, attempt, "batch migration missing template, batch_size, or valid cursor_column")
		return exec.Kind, migrationexecution.StatusFailed
	}
	if err := exec.Update().SetSQLExecuted(template).SetStatus(migrationexecution.StatusRunning).Exec(ctx); err != nil {
		r.failExecution(ctx, job, workerID, exec, attempt, "mark running: "+err.Error())
		return exec.Kind, migrationexecution.StatusFailed
	}
	if attempt != nil {
		_ = attempt.Update().SetSQLExecuted(template).Exec(ctx)
	}
	log.Info().Str("event", "exec_start").Str("status", "running").
		Str("migration_id", fmt.Sprint(mig.ID)).Str("kind", string(exec.Kind)).
		Str("sql", truncate(template)).Msg("starting batch migration")

	if err := r.captureSnapshot(ctx, t, exec.ID, mig.ID, schemasnapshot.PhaseBefore); err != nil {
		r.failExecution(ctx, job, workerID, exec, attempt, "before snapshot: "+err.Error())
		return exec.Kind, migrationexecution.StatusFailed
	}

	// Resume from the job checkpoint (reclaim/retry) or start below min_id.
	// min_id/max_id default to 1 / last committed cursor + 1 when unset
	// (openapi allows omitting them; the template's {{max_id}} then bounds
	// the scope via the committed cursor).
	minID := int64(1)
	if mig.MinID != nil {
		minID = *mig.MinID
	}
	last := minID - 1
	if job.BatchCursor != nil && *job.BatchCursor > last {
		last = *job.BatchCursor
	}
	maxID := minID
	if mig.MaxID != nil {
		maxID = *mig.MaxID
	} else if job.BatchCursor != nil {
		maxID = *job.BatchCursor + 1
	}
	batchesDone := job.BatchesDone
	rowsDone := job.RowsDone
	batchSize := int64(mig.BatchSize)

	render := func(l, u int64) (string, error) {
		r := strings.NewReplacer(
			"{{last_id}}", strconv.FormatInt(l, 10),
			"{{upper_id}}", strconv.FormatInt(u, 10),
			"{{batch_size}}", strconv.FormatInt(batchSize, 10),
			"{{min_id}}", strconv.FormatInt(minID, 10),
			"{{max_id}}", strconv.FormatInt(maxID, 10),
		)
		return r.Replace(template), nil
	}

	dialect := target.NewPostgresDialect()
	if _, err := dialect.QuoteIdent(mig.CursorColumn); err != nil {
		r.failExecution(ctx, job, workerID, exec, attempt, "invalid cursor column: "+mig.CursorColumn)
		return exec.Kind, migrationexecution.StatusFailed
	}

	for {
		cancelled, err := r.queue.IsCancelled(ctx, job.ID)
		if err != nil {
			log.Warn().Err(err).Str("event", "cancel_check_error").Msg("cancel check failed; continuing")
		}
		if cancelled {
			status := migrationexecution.StatusCancelled
			if batchesDone > 0 {
				status = migrationexecution.StatusPartial
			}
			return r.stopBatch(ctx, log, workerID, job, exec, mig, attempt, status, batchesDone, rowsDone)
		}

		// Probe the next upper bound: the smallest cursor value above `last`
		// (so the rendered WHERE ... > last AND <= upper covers exactly up to
		// batch_size rows), or max+1 when the tail is smaller than a batch.
		upper, err := r.probeUpper(ctx, t, mig.BatchTable, mig.CursorColumn, last, maxID, batchSize)
		if err != nil {
			r.failExecution(ctx, job, workerID, exec, attempt, "cursor probe: "+err.Error())
			return exec.Kind, migrationexecution.StatusFailed
		}
		if upper > maxID {
			break // no rows left in (last, max_id]
		}

		statement, rErr := render(last, upper)
		if rErr != nil {
			r.failExecution(ctx, job, workerID, exec, attempt, "render template: "+rErr.Error())
			return exec.Kind, migrationexecution.StatusFailed
		}

		batchStart := time.Now()
		affected, err := t.ExecuteArgs(ctx, statement, nil)
		if err != nil {
			log.Error().Err(err).Str("event", "batch_error").Str("sql", truncate(statement)).Msg("batch failed")
			r.failExecution(ctx, job, workerID, exec, attempt, err.Error())
			return exec.Kind, migrationexecution.StatusFailed
		}
		batchesDone++
		rowsDone += affected
		metrics.BatchesCommitted.Inc()
		metrics.BatchRows.Add(float64(affected))
		log.Info().
			Str("event", "batch_commit").
			Int("batch", batchesDone).
			Int64("rows", affected).
			Int64("duration_ms", time.Since(batchStart).Milliseconds()).
			Msg("batch committed")
		if attempt != nil {
			// Live progress on the running attempt row; terminal state lands
			// once via finalizeAttempt.
			_ = attempt.Update().SetBatchesDone(batchesDone).SetRowsDone(rowsDone).Exec(ctx)
		}

		// Checkpoint AFTER commit; on error abort without touching statuses —
		// the job goes stale and gets reclaimed, re-running this batch
		// (templates must be idempotent).
		if err := r.queue.Checkpoint(ctx, job.ID, workerID, upper, batchesDone, rowsDone); err != nil {
			log.Error().Err(err).Str("event", "checkpoint_error").Msg("checkpoint failed; aborting, job will be reclaimed")
			return exec.Kind, migrationexecution.StatusRunning
		}
		_ = exec.Update().SetBatchCursor(upper).SetBatchesDone(batchesDone).SetRowsDone(rowsDone).Exec(ctx)

		last = upper
	}

	if err := r.captureSnapshot(ctx, t, exec.ID, mig.ID, schemasnapshot.PhaseAfter); err != nil {
		log.Warn().Err(err).Str("event", "snapshot_error").Msg("after-snapshot failed; execution still succeeds")
	}

	finish := time.Now()
	if err := exec.Update().
		SetStatus(migrationexecution.StatusSuccess).
		SetNillableFinishedAt(&finish).
		SetBatchCursor(maxID).
		SetBatchesDone(batchesDone).
		SetRowsDone(rowsDone).
		Exec(ctx); err != nil {
		r.failExecution(ctx, job, workerID, exec, attempt, "mark success: "+err.Error())
		return exec.Kind, migrationexecution.StatusFailed
	}
	_ = r.client.Migration.Update().Where(migration.ID(mig.ID)).SetStatus(migration.StatusSuccess).Exec(ctx)
	r.finalizeAttempt(ctx, attempt, migrationattempt.StatusSuccess, template, "", &maxID)
	if err := r.queue.FinishSuccess(ctx, job.ID, workerID); err != nil {
		log.Error().Err(err).Str("event", "finish_error").Msg("job finish failed; execution stays success")
	}
	log.Info().Int("batches", batchesDone).Int64("rows", rowsDone).Msg("batch migration finished")
	return exec.Kind, migrationexecution.StatusSuccess
}

// stopBatch lands the terminal "stopped" state (cancelled or partial) on
// execution, migration, attempt, and job.
func (r *Runner) stopBatch(ctx context.Context, log zerolog.Logger, workerID string, job *queue.ClaimedJob, exec *ent.MigrationExecution, mig *ent.Migration, attempt *ent.MigrationAttempt, status migrationexecution.Status, batchesDone int, rowsDone int64) (migrationexecution.Kind, migrationexecution.Status) {
	finish := time.Now()
	if err := exec.Update().
		SetStatus(status).
		SetNillableFinishedAt(&finish).
		SetBatchesDone(batchesDone).
		SetRowsDone(rowsDone).
		Exec(ctx); err != nil {
		log.Error().Err(err).Str("event", "stop_error").Msg("stop update failed")
	}
	migStatus := migration.StatusCancelled
	if status == migrationexecution.StatusPartial {
		migStatus = migration.StatusPartial
	}
	_ = r.client.Migration.Update().Where(migration.ID(mig.ID)).SetStatus(migStatus).Exec(ctx)

	// Attempt history keeps the stopped outcome (partial rows stay visible).
	attemptStatus := migrationattempt.StatusCancelled
	if status == migrationexecution.StatusPartial {
		attemptStatus = migrationattempt.StatusPartial
	}
	cursor := exec.BatchCursor
	r.finalizeAttempt(ctx, attempt, attemptStatus, "", "", cursor)

	// Job mirrors: partial keeps the checkpoint for a later retry/revert.
	jobStatus := "cancelled"
	if status == migrationexecution.StatusPartial {
		jobStatus = "partial"
	}
	_ = r.finishJobStatus(ctx, job.ID, workerID, jobStatus)
	return exec.Kind, status
}

// finishJobStatus writes a terminal job status directly (cancel/partial path;
// FinishSuccess/FinishFailed cover the rest).
func (r *Runner) finishJobStatus(ctx context.Context, jobID int, workerID string, status string) error {
	metrics.ObserveOutcome(status)
	return r.client.MigrationJob.Update().
		Where(migrationjob.ID(jobID), migrationjob.LockedBy(workerID)).
		SetStatus(migrationjob.Status(status)).
		SetNillableFinishedAt(now()).
		ClearLockedBy().
		Exec(ctx)
}

// probeUpper returns MIN(cursor) above last within (last, max_id], or
// max+1 when the remaining tail is empty. Table and cursor column are
// validated and dialect-quoted before interpolation; values are plain integers.
// limit bounds the batch: the probe returns the batch_size-th distinct cursor
// value above `last` so each batch touches at most `limit` rows.
func (r *Runner) probeUpper(ctx context.Context, t *target.Target, table, cursorColumn string, last, maxID, limit int64) (int64, error) {
	tbl, err := t.Dialect().QuoteIdent(table)
	if err != nil {
		return 0, fmt.Errorf("invalid batch table %q: %w", table, err)
	}
	col, err := t.Dialect().QuoteIdent(cursorColumn)
	if err != nil {
		return 0, fmt.Errorf("invalid cursor column %q: %w", cursorColumn, err)
	}
	// The batch_size-th row above `last`: subselect ordered ascending with
	// LIMIT, then take MAX — gives the inclusive upper bound of this batch.
	_, rows, err := t.Query(ctx,
		fmt.Sprintf("SELECT COALESCE(MAX(id), %d) FROM (SELECT %s AS id FROM %s WHERE %s > %d AND %s <= %d ORDER BY %s ASC LIMIT %d) b",
			maxID+1, col, tbl, col, last, col, maxID, col, limit), 1)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 || len(rows[0]) == 0 {
		return 0, fmt.Errorf("cursor probe returned no rows")
	}
	return toInt64(rows[0][0]), nil
}

// toInt64 widens the numeric shapes database/sql may hand back for MIN().
func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int32:
		return int64(n)
	case float64:
		return int64(n)
	case []byte:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(string(n)), 10, 64)
		return parsed
	default:
		return 0
	}
}

// captureSnapshot introspects the target and stores the JSON blob as one
// phase of the execution's snapshot pair (worker-local copy of the handler
// pattern; the worker must not import internal/handler).
func (r *Runner) captureSnapshot(ctx context.Context, t *target.Target, executionID, migrationID int, phase schemasnapshot.Phase) error {
	schema, err := t.IntrospectSchema(ctx)
	if err != nil {
		return err
	}
	blob, err := json.Marshal(schema)
	if err != nil {
		return err
	}
	return r.client.SchemaSnapshot.Create().
		SetExecutionID(executionID).
		SetMigrationID(migrationID).
		SetPhase(phase).
		SetJSON(string(blob)).
		Exec(ctx)
}

// truncate caps logged SQL so huge templates never flood the logs.
func truncate(s string) string {
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}
