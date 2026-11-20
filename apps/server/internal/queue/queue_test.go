package queue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/rs/zerolog"

	"Orpbal/ent"
	"Orpbal/ent/migration"
	"Orpbal/ent/migrationexecution"
)

var testSeq atomic.Int64

// newQueueEnv opens an isolated schema, creates the ent tables and seeds one
// connection + user + migration + queued execution/job chain, returning the
// Queue and a raw handle for backdating rows.
func newQueueEnv(t *testing.T) (*Queue, *sql.DB, *ent.Client, seed) {
	t.Helper()
	rawDSN := os.Getenv("TEST_DATABASE_URL")
	if rawDSN == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	parsed, err := url.Parse(rawDSN)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	adminDB, err := sql.Open("postgres", rawDSN)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	schema := fmt.Sprintf("qtest_%d_%d", os.Getpid(), testSeq.Add(1))
	if _, err := adminDB.Exec("CREATE SCHEMA " + schema); err != nil {
		_ = adminDB.Close()
		t.Fatalf("create test schema: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	dsn := parsed.String()

	client, err := ent.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("ent.Open() error = %v", err)
	}
	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatalf("Schema.Create() error = %v", err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("queue sql.Open() error = %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
		_ = client.Close()
		_, _ = adminDB.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
		_ = adminDB.Close()
	})

	q := New(db, zerolog.Nop())
	seedRows, err := seedFixtures(client)
	if err != nil {
		t.Fatalf("seed fixtures: %v", err)
	}
	return q, db, client, seedRows
}

type seed struct {
	connectionID int
	jobID        int
	executionID  int
}

func seedFixtures(client *ent.Client) (seed, error) {
	ctx := context.Background()
	var s seed
	conn, err := client.DatabaseConnection.Create().
		SetName(fmt.Sprintf("qconn-%d", testSeq.Add(1))).
		SetHost("localhost").SetDatabaseName("target").SetUsername("u").
		Save(ctx)
	if err != nil {
		return s, err
	}
	s.connectionID = conn.ID
	user, err := client.PlatformUser.Create().
		SetUsername(fmt.Sprintf("quser-%d", testSeq.Add(1))).
		SetPasswordHash("x").
		Save(ctx)
	if err != nil {
		return s, err
	}
	mig, err := client.Migration.Create().
		SetConnectionID(conn.ID).
		SetVersion(int(testSeq.Add(1))).
		SetSQLUp("SELECT 1").
		SetStatus(migration.StatusQueued).
		SetAuthorID(user.ID).
		Save(ctx)
	if err != nil {
		return s, err
	}
	exec, err := client.MigrationExecution.Create().
		SetMigrationID(mig.ID).
		SetConnectionID(conn.ID).
		SetKind(migrationexecution.KindUp).
		SetStatus(migrationexecution.StatusQueued).
		SetActorID(user.ID).
		Save(ctx)
	if err != nil {
		return s, err
	}
	s.executionID = exec.ID
	job, err := client.MigrationJob.Create().
		SetExecutionID(exec.ID).
		SetConnectionID(conn.ID).
		Save(ctx)
	if err != nil {
		return s, err
	}
	s.jobID = job.ID
	return s, nil
}

func jobStatus(t *testing.T, db *sql.DB, jobID int) string {
	t.Helper()
	var status string
	if err := db.QueryRow("SELECT status FROM migration_jobs WHERE id=$1", jobID).Scan(&status); err != nil {
		t.Fatalf("job status: %v", err)
	}
	return status
}

func execStatus(t *testing.T, db *sql.DB, execID int) string {
	t.Helper()
	var status string
	if err := db.QueryRow("SELECT status FROM migration_executions WHERE id=$1", execID).Scan(&status); err != nil {
		t.Fatalf("exec status: %v", err)
	}
	return status
}

func TestClaimAndFinishSuccess(t *testing.T) {
	q, db, _, s := newQueueEnv(t)
	ctx := context.Background()

	job, err := q.Claim(ctx, "w1", 30*time.Second)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	defer job.Release()
	if job.ID != s.jobID || job.ExecutionID != s.executionID || job.ConnectionID != s.connectionID {
		t.Fatalf("claimed wrong job: %+v, want id=%d exec=%d conn=%d", job, s.jobID, s.executionID, s.connectionID)
	}
	if job.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", job.Attempts)
	}
	if got := jobStatus(t, db, job.ID); got != "running" {
		t.Fatalf("job status = %s, want running", got)
	}

	// Second claim must not see the claimed row.
	if _, err := q.Claim(ctx, "w2", 30*time.Second); !errors.Is(err, ErrNotClaimed) {
		t.Fatalf("second claim = %v, want ErrNotClaimed", err)
	}

	if err := q.FinishSuccess(ctx, job.ID, "w2"); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("FinishSuccess by wrong worker = %v, want ErrNotOwner", err)
	}
	if err := q.FinishSuccess(ctx, job.ID, "w1"); err != nil {
		t.Fatalf("FinishSuccess: %v", err)
	}
	if got := jobStatus(t, db, job.ID); got != "success" {
		t.Fatalf("job status = %s, want success", got)
	}
	var finished *time.Time
	var lockedBy sql.NullString
	if err := db.QueryRow("SELECT finished_at, locked_by FROM migration_jobs WHERE id=$1", job.ID).Scan(&finished, &lockedBy); err != nil {
		t.Fatalf("finished_at: %v", err)
	}
	if finished == nil || lockedBy.Valid {
		t.Fatalf("finished_at=%v locked_by=%v, want finished set and lock released", finished, lockedBy)
	}
}

func TestStaleHeartbeatReclaim(t *testing.T) {
	q, db, _, _ := newQueueEnv(t)
	ctx := context.Background()

	reset := func() {
		t.Helper()
		if _, err := db.Exec("UPDATE migration_jobs SET status='queued', locked_by=NULL, attempts=0, heartbeat_at=NULL, claimed_at=NULL WHERE id=(SELECT min(id) FROM migration_jobs)"); err != nil {
			t.Fatalf("reset: %v", err)
		}
	}
	reset()

	job, err := q.Claim(ctx, "w1", 30*time.Second)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	job.Release()

	// Backdate the heartbeat so the job looks abandoned.
	if _, err := db.Exec("UPDATE migration_jobs SET heartbeat_at = now() - interval '10 minutes' WHERE id=$1", job.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	reclaimed, err := q.Claim(ctx, "w2", 60*time.Second)
	if err != nil {
		t.Fatalf("reclaim Claim: %v", err)
	}
	if reclaimed.ID != job.ID {
		reclaimed.Release()
		t.Fatalf("reclaimed job id = %d, want %d", reclaimed.ID, job.ID)
	}
	if reclaimed.Attempts != 2 {
		t.Fatalf("attempts after reclaim = %d, want 2", reclaimed.Attempts)
	}
	var lockedBy string
	if err := db.QueryRow("SELECT locked_by FROM migration_jobs WHERE id=$1", job.ID).Scan(&lockedBy); err != nil {
		t.Fatalf("locked_by: %v", err)
	}
	if lockedBy != "w2" {
		t.Fatalf("locked_by = %s, want w2", lockedBy)
	}

	// A live heartbeat blocks reclaim even with a short TTL.
	reclaimed.Release()
	reset()
	fresh, err := q.Claim(ctx, "w3", 30*time.Second)
	if err != nil {
		t.Fatalf("fresh Claim: %v", err)
	}
	if err := q.Heartbeat(ctx, fresh.ID, "w3"); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if _, err := q.Claim(ctx, "w4", 1*time.Second); !errors.Is(err, ErrNotClaimed) {
		t.Fatalf("claim after fresh heartbeat = %v, want ErrNotClaimed", err)
	}
	if err := q.Heartbeat(ctx, fresh.ID, "w4"); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("Heartbeat by wrong worker = %v, want ErrNotOwner", err)
	}
	fresh.Release()
}

func TestCheckpointGuard(t *testing.T) {
	q, _, _, s := newQueueEnv(t)
	ctx := context.Background()

	job, err := q.Claim(ctx, "w1", 30*time.Second)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	defer job.Release()
	if job.ID != s.jobID {
		t.Fatalf("claimed job %d, want %d", job.ID, s.jobID)
	}

	if err := q.Checkpoint(ctx, job.ID, "w2", 100, 3, 500); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("Checkpoint by wrong worker = %v, want ErrNotOwner", err)
	}
	if err := q.Checkpoint(ctx, job.ID, "w1", 100, 3, 500); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}
	got, err := q.Claim(ctx, "w3", 30*time.Second) // must NOT return the running job again
	if err == nil {
		got.Release()
		t.Fatalf("claimed already-running job %+v", got)
	}
}

func TestFinishFailedTransientRetries(t *testing.T) {
	q, db, _, s := newQueueEnv(t)
	ctx := context.Background()

	job, err := q.Claim(ctx, "w1", 30*time.Second)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	job.Release()

	// Transient deadlock error: attempts=1 < 3, so back to queued.
	retry, err := q.FinishFailed(ctx, job.ID, "w1", "pq: deadlock detected (40P01)")
	if err != nil {
		t.Fatalf("FinishFailed: %v", err)
	}
	if !retry {
		t.Fatal("willRetry = false, want true")
	}
	if got := jobStatus(t, db, job.ID); got != "queued" {
		t.Fatalf("job status = %s, want queued", got)
	}
	if got := execStatus(t, db, s.executionID); got != "queued" {
		t.Fatalf("execution status = %s, want queued", got)
	}

	// Drive it to terminal: two more transient failures exhaust the budget.
	for i := 2; i <= 3; i++ {
		again, err := q.Claim(ctx, "w2", 30*time.Second)
		if err != nil {
			t.Fatalf("attempt %d Claim: %v", i, err)
		}
		if again.ID != job.ID {
			again.Release()
			t.Fatalf("attempt %d claimed job %d, want %d", i, again.ID, job.ID)
		}
		willRetry, err := q.FinishFailed(ctx, again.ID, "w2", "pq: deadlock detected (40P01)")
		again.Release()
		if err != nil {
			t.Fatalf("attempt %d FinishFailed: %v", i, err)
		}
		wantRetry := i < 3
		if willRetry != wantRetry {
			t.Fatalf("attempt %d willRetry = %v, want %v", i, willRetry, wantRetry)
		}
	}
	if got := jobStatus(t, db, job.ID); got != "failed" {
		t.Fatalf("job status after 3 attempts = %s, want failed", got)
	}
	if got := execStatus(t, db, s.executionID); got != "failed" {
		t.Fatalf("execution status after 3 attempts = %s, want failed", got)
	}
	var errMsg string
	if err := db.QueryRow("SELECT error FROM migration_jobs WHERE id=$1", job.ID).Scan(&errMsg); err != nil {
		t.Fatalf("error text: %v", err)
	}
	if errMsg == "" {
		t.Fatal("error text not stored on terminal failure")
	}
}

func TestFinishFailedTerminalNonTransient(t *testing.T) {
	q, db, _, s := newQueueEnv(t)
	ctx := context.Background()

	job, err := q.Claim(ctx, "w1", 30*time.Second)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	job.Release()

	retry, err := q.FinishFailed(ctx, job.ID, "w1", "pq: relation \"nope\" does not exist (42P01)")
	if err != nil {
		t.Fatalf("FinishFailed: %v", err)
	}
	if retry {
		t.Fatal("non-transient error must not retry")
	}
	if got := jobStatus(t, db, job.ID); got != "failed" {
		t.Fatalf("job status = %s, want failed", got)
	}
	if got := execStatus(t, db, s.executionID); got != "failed" {
		t.Fatalf("execution status = %s, want failed", got)
	}
}

func TestRequestCancelAndIsCancelled(t *testing.T) {
	q, db, _, _ := newQueueEnv(t)
	ctx := context.Background()

	job, err := q.Claim(ctx, "w1", 30*time.Second)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	defer job.Release()

	if err := q.RequestCancel(ctx, job.ID); err != nil {
		t.Fatalf("RequestCancel on running: %v", err)
	}
	if got := jobStatus(t, db, job.ID); got != "cancelling" {
		t.Fatalf("job status = %s, want cancelling", got)
	}
	ok, err := q.IsCancelled(ctx, job.ID)
	if err != nil {
		t.Fatalf("IsCancelled: %v", err)
	}
	if !ok {
		t.Fatal("IsCancelled = false, want true")
	}
	// Job now in cancelling: still checkpointable, but not claimable or re-cancellable via queued path.
	if err := q.Checkpoint(ctx, job.ID, "w1", 5, 1, 10); err != nil {
		t.Fatalf("Checkpoint while cancelling: %v", err)
	}
	// Terminal transition rejects further cancels.
	if _, err := db.Exec("UPDATE migration_jobs SET status='failed', locked_by=NULL WHERE id=$1", job.ID); err != nil {
		t.Fatalf("force failed: %v", err)
	}
	if err := q.RequestCancel(ctx, job.ID); !errors.Is(err, ErrTerminal) {
		t.Fatalf("RequestCancel on failed = %v, want ErrTerminal", err)
	}

	// Queued job goes straight to cancelled.
	if _, err := db.Exec("UPDATE migration_jobs SET status='queued', locked_by=NULL, heartbeat_at=NULL WHERE id=$1", job.ID); err != nil {
		t.Fatalf("reset queued: %v", err)
	}
	if err := q.RequestCancel(ctx, job.ID); err != nil {
		t.Fatalf("RequestCancel on queued: %v", err)
	}
	if got := jobStatus(t, db, job.ID); got != "cancelled" {
		t.Fatalf("queued job status = %s, want cancelled", got)
	}
}
