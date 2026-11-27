// Package queue implements the migration_job claim/progress/finish protocol
// against the platform Postgres DB using raw SQL (ent is unsuitable for the
// FOR UPDATE SKIP LOCKED claim statement).
package queue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/rs/zerolog"

	"Orpbal/internal/metrics"
)

// Table is the migration job table name (ent default pluralization).
const table = "migration_jobs"

// maxAttempts is how many runs (claims) a job gets before failing terminally.
const maxAttempts = 3

// ClaimedJob is one lease of a migration job by a worker.
type ClaimedJob struct {
	ID           int
	ExecutionID  int
	ConnectionID int
	Attempts     int
	Traceparent  string
	BatchCursor  *int64
	BatchesDone  int
	RowsDone     int64

	// conn holds the dedicated platform connection carrying the per-target
	// advisory lock; Release unlocks and returns it to the pool.
	conn *sql.Conn
}

// Release frees the advisory lock taken on claim. Safe to call more than
// once; always call it (typically via defer) for every claimed job.
func (j *ClaimedJob) Release() {
	if j == nil || j.conn == nil {
		return
	}
	_, _ = j.conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock(hashtext('orpbal:conn:' || $1))", j.ConnectionID)
	_ = j.conn.Close()
	j.conn = nil
}

// Queue drives the migration_jobs protocol over the platform DB pool.
type Queue struct {
	db     *sql.DB
	logger zerolog.Logger
}

// New builds a Queue on top of the platform DB pool.
func New(db *sql.DB, logger zerolog.Logger) *Queue {
	return &Queue{db: db, logger: logger}
}

// ErrNotClaimed is returned when no work is available.
var ErrNotClaimed = errors.New("queue: no claimable job")

// ErrNotOwner is returned by Checkpoint when the job is not locked by this worker.
var ErrNotOwner = errors.New("queue: job not locked by this worker")

// ErrTerminal is returned by RequestCancel when the job already finished.
var ErrTerminal = errors.New("queue: job already terminal")

// claimSQL claims the next eligible job. The partial gate holds a NEW
// migration on a connection whose last run ended 'partial' (some batches
// committed): while such a state exists on the connection, only retry and
// batch_revert jobs — the designated ways out of the partial state — are
// claimable (spec: hold the next migration while the previous one is
// partial/unresolved).
const claimSQL = `UPDATE ` + table + ` SET
	status='running', locked_by=$1, claimed_at=now(), heartbeat_at=now(),
	attempts=attempts+1, started_at=COALESCE(started_at, now())
WHERE id = (
	SELECT id FROM ` + table + `
	WHERE (status='queued'
	    OR (status='running' AND heartbeat_at < now() - $2::interval))
	  AND (
		NOT EXISTS (
			SELECT 1 FROM ` + table + ` j2
			WHERE j2.connection_id = ` + table + `.connection_id
			  AND j2.status = 'partial'
		)
		OR (SELECT e.kind FROM migration_executions e WHERE e.id = ` + table + `.execution_id)
			IN ('retry', 'batch_revert')
	  )
	ORDER BY created_at
	FOR UPDATE SKIP LOCKED
	LIMIT 1
)
RETURNING id, execution_id, connection_id, attempts,
	COALESCE(traceparent, ''), batch_cursor, batches_done, rows_done`

// Claim atomically leases the next queued job (or one whose worker died,
// heartbeat older than lockTTL) and takes the per-connection advisory lock on
// a dedicated checked-out platform connection. The caller MUST Release.
func (q *Queue) Claim(ctx context.Context, workerID string, lockTTL time.Duration) (*ClaimedJob, error) {
	conn, err := q.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("queue: checkout conn: %w", err)
	}

	var (
		job      ClaimedJob
		tracep   sql.NullString
		batchCur sql.NullInt64
	)
	err = conn.QueryRowContext(ctx, claimSQL, workerID, int64(lockTTL/time.Second)).
		Scan(&job.ID, &job.ExecutionID, &job.ConnectionID, &job.Attempts, &tracep, &batchCur, &job.BatchesDone, &job.RowsDone)
	if errors.Is(err, sql.ErrNoRows) {
		_ = conn.Close()
		return nil, ErrNotClaimed
	}
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("queue: claim: %w", err)
	}

	// Advisory lock second: if another worker already runs this connection,
	// put the job back so it isn't held hostage by a losing claimer.
	var locked bool
	if err := conn.QueryRowContext(ctx,
		"SELECT pg_try_advisory_lock(hashtext('orpbal:conn:' || $1))", job.ConnectionID,
	).Scan(&locked); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("queue: advisory lock: %w", err)
	}
	if !locked {
		if _, err := conn.ExecContext(ctx,
			"UPDATE "+table+" SET status='queued', locked_by=NULL, heartbeat_at=NULL WHERE id=$1 AND locked_by=$2",
			job.ID, workerID); err != nil {
			q.logger.Error().Err(err).Int("job_id", job.ID).Msg("queue: unlock-contended revert failed")
		}
		_ = conn.Close()
		return nil, ErrNotClaimed
	}

	job.Traceparent = tracep.String
	if batchCur.Valid {
		v := batchCur.Int64
		job.BatchCursor = &v
	}
	job.conn = conn
	metrics.JobsClaimed.WithLabelValues(workerID).Inc()
	return &job, nil
}

// Heartbeat refreshes the job's lease timestamp; the worker calls it every ~5s.
func (q *Queue) Heartbeat(ctx context.Context, jobID int, workerID string) error {
	res, err := q.db.ExecContext(ctx,
		"UPDATE "+table+" SET heartbeat_at=now() WHERE id=$1 AND locked_by=$2 AND status IN ('running','cancelling')",
		jobID, workerID)
	if err != nil {
		return fmt.Errorf("queue: heartbeat: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotOwner
	}
	return nil
}

// Checkpoint stores batch progress; guarded so only the owning worker (and
// only while the job is running or being cancelled) can advance it.
func (q *Queue) Checkpoint(ctx context.Context, jobID int, workerID string, batchCursor int64, batchesDone int, rowsDone int64) error {
	res, err := q.db.ExecContext(ctx,
		"UPDATE "+table+" SET batch_cursor=$3, batches_done=$4, rows_done=$5 WHERE id=$1 AND locked_by=$2 AND status IN ('running','cancelling')",
		jobID, workerID, batchCursor, batchesDone, rowsDone)
	if err != nil {
		return fmt.Errorf("queue: checkpoint: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotOwner
	}
	return nil
}

// FinishSuccess marks the job success. The execution status itself is owned
// by the worker; the job row only mirrors completion.
func (q *Queue) FinishSuccess(ctx context.Context, jobID int, workerID string) error {
	res, err := q.db.ExecContext(ctx,
		"UPDATE "+table+" SET status='success', finished_at=now(), locked_by=NULL WHERE id=$1 AND locked_by=$2 AND status IN ('running','cancelling')",
		jobID, workerID)
	if err != nil {
		return fmt.Errorf("queue: finish success: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotOwner
	}
	metrics.ObserveOutcome("success")
	return nil
}

// isTransient reports whether the failure message carries a pq error class
// meaning the run failed environmentally (deadlock/serialization 40XXX,
// connection exception 08XXX, operator intervention 57000) and is worth
// retrying. The worker hands us its captured target-DB error, so we match on
// the pq error text rather than a live *pq.Error value.
var transientClass = regexp.MustCompile(`\b(40|08|57)[0-9A-Z]{3}\b`)

func isTransient(errMsg string) bool {
	return transientClass.MatchString(errMsg)
}

// FinishFailed marks the job failed, mirroring the failure onto the
// execution. If the cause is transient and attempts remain, the job returns
// to queued for another worker to pick up (willRetry=true). The execution
// row keeps its failed status and error (the live view of the last attempt);
// the per-attempt outcome itself is append-only history in
// migration_attempts written by the worker, so retrying never overwrites or
// erases what already ran (M2: attempt history must not be overwritten).
func (q *Queue) FinishFailed(ctx context.Context, jobID int, workerID string, errMsg string) (willRetry bool, err error) {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("queue: finish failed: %w", err)
	}
	defer func() {
		if pErr := tx.Rollback(); pErr != nil && !errors.Is(pErr, sql.ErrTxDone) {
			q.logger.Error().Err(pErr).Int("job_id", jobID).Msg("queue: finish-failed rollback")
		}
	}()

	var attempts int
	err = tx.QueryRowContext(ctx,
		"SELECT attempts FROM "+table+" WHERE id=$1 AND locked_by=$2 FOR UPDATE", jobID, workerID).Scan(&attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotOwner
	}
	if err != nil {
		return false, fmt.Errorf("queue: finish failed select: %w", err)
	}

	retry := attempts < maxAttempts && isTransient(errMsg)
	// The job mirrors the run: terminal failure closes it, a retry keeps it
	// queued. The execution row is NOT reset to queued here — the worker
	// flips it back to running when the retry actually starts.
	jobStatus := "failed"
	if retry {
		jobStatus = "queued"
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE "+table+" SET status=$3::text, error=$4, finished_at=CASE WHEN $3::text='failed' THEN now() ELSE NULL END, locked_by=NULL WHERE id=$1 AND locked_by=$2",
		jobID, workerID, jobStatus, errMsg); err != nil {
		return false, fmt.Errorf("queue: finish failed job update: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE migration_executions SET status='failed', error=$2, finished_at=COALESCE(finished_at, now()) WHERE id=(SELECT execution_id FROM "+table+" WHERE id=$1)",
		jobID, errMsg); err != nil {
		return false, fmt.Errorf("queue: finish failed execution update: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("queue: finish failed commit: %w", err)
	}
	if retry {
		metrics.ObserveOutcome("retry")
	} else {
		metrics.ObserveOutcome("failed")
	}
	return retry, nil
}

// RequestCancel transitions a queued job straight to cancelled, or a running
// one to cancelling so the worker stops between batches. Terminal states are
// rejected.
func (q *Queue) RequestCancel(ctx context.Context, jobID int) error {
	res, err := q.db.ExecContext(ctx,
		"UPDATE "+table+" SET status=CASE WHEN status='queued' THEN 'cancelled' ELSE 'cancelling' END WHERE id=$1 AND status IN ('queued','running')",
		jobID)
	if err != nil {
		return fmt.Errorf("queue: request cancel: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTerminal
	}
	// A queued job is terminal right away; a running job lands its outcome
	// when the worker records the stopped state.
	var status string
	if err := q.db.QueryRowContext(ctx,
		"SELECT status FROM "+table+" WHERE id=$1", jobID).Scan(&status); err == nil && status == "cancelled" {
		metrics.ObserveOutcome("cancelled")
	}
	return nil
}

// IsCancelled reports whether the job is in cancelling (worker should stop).
func (q *Queue) IsCancelled(ctx context.Context, jobID int) (bool, error) {
	var status string
	if err := q.db.QueryRowContext(ctx,
		"SELECT status FROM "+table+" WHERE id=$1", jobID).Scan(&status); err != nil {
		return false, fmt.Errorf("queue: is cancelled: %w", err)
	}
	return status == "cancelling", nil
}
