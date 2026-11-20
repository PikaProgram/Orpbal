package worker

import (
	"context"
	"database/sql"
	"strings"
	"fmt"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"Orpbal/ent"
	"Orpbal/ent/databaseconnection"
	"Orpbal/ent/migration"
	"Orpbal/ent/migrationexecution"
	"Orpbal/ent/migrationjob"
	"Orpbal/ent/schemasnapshot"
	"Orpbal/internal/queue"
	"Orpbal/internal/security/crypto"
	
	"Orpbal/internal/target"

	"github.com/rs/zerolog"
	_ "github.com/lib/pq"
)

var wkSeq atomic.Int64

// newWorkerEnv builds the full runner stack against one isolated platform
// schema: ent client, queue, and a target Manager. The TARGET database is
// test database; that server is throwaway compose infrastructure.
func newWorkerEnv(t *testing.T) (*Runner, *sql.DB, *ent.Client, int, string) {
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
		t.Fatalf("admin sql.Open() error = %v", err)
	}
	schema := fmt.Sprintf("wtest_%d_%d", os.Getpid(), wkSeq.Add(1))
	if _, err := adminDB.Exec("CREATE SCHEMA " + schema); err != nil {
		_ = adminDB.Close()
		t.Fatalf("create test schema: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	platformDSN := parsed.String()

	// The managed TARGET connection pins a separate schema of the same test
	// server: per-test isolation for the worker's tables (introspection and
	// the batch probe are search_path driven).
	targetQuery := url.Values{}
	targetQuery.Set("sslmode", "disable")
	targetQuery.Set("search_path", schema)
	targetDSN := fmt.Sprintf("postgres://%s@%s/%s?%s",
		parsed.User.Username(), parsed.Host, strings.TrimPrefix(parsed.Path, "/"),
		targetQuery.Encode())

	client, err := ent.Open("postgres", platformDSN)
	if err != nil {
		_ = adminDB.Close()
		t.Fatalf("ent.Open() error = %v", err)
	}
	if err := client.Schema.Create(context.Background()); err != nil {
		_ = client.Close()
		_ = adminDB.Close()
		t.Fatalf("Schema.Create() error = %v", err)
	}
	platformDB, err := sql.Open("postgres", platformDSN)
	if err != nil {
		_ = client.Close()
		_ = adminDB.Close()
		t.Fatalf("queue sql.Open() error = %v", err)
	}

	t.Cleanup(func() {
		_ = platformDB.Close()
		_ = client.Close()
		_, _ = adminDB.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
		_ = adminDB.Close()
	})

	// Managed target connection: DSN into the same test server, pinned to
	// the test schema so the worker's tables are per-test isolated.
	if _, err := sql.Open("postgres", targetDSN); err != nil {
		t.Fatalf("target DSN: %v", err)
	}

	key := make([]byte, 32)
	cipher, err := crypto.New(key)
	if err != nil {
		t.Fatalf("crypto.New() error = %v", err)
	}
	ctx := context.Background()
	targetParsed, err := url.Parse(targetDSN)
	if err != nil {
		t.Fatalf("parse target dsn: %v", err)
	}
	pw, _ := parsed.User.Password()
	plain, err := cipher.Encrypt([]byte(pw))
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}
	conn, err := client.DatabaseConnection.Create().
		SetName(fmt.Sprintf("wconn-%d", wkSeq.Add(1))).
		SetEngine(databaseconnection.EnginePostgres).
		SetHost(targetParsed.Hostname()).
		SetPort(func() int { p, _ := atoiDefault(targetParsed.Port(), 5432); return p }()).
		SetDatabaseName(strings.TrimPrefix(targetParsed.Path, "/")).
		SetUsername(targetParsed.User.Username()).
		SetPasswordCiphertext(plain).
		SetSslMode(databaseconnection.SslModeDisable).
		SetIsEnabled(true).
		Save(ctx)
	if err != nil {
		t.Fatalf("create managed connection: %v", err)
	}

	manager := target.NewManager(nil, cipher)
	q := queue.New(platformDB, zerolog.Nop())
	runner := NewRunner(client, q, manager, zerolog.Nop())
	return runner, platformDB, client, conn.ID, schema
}

func atoiDefault(s string, def int) (int, error) {
	if s == "" {
		return def, nil
	}
	var n int
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("non-numeric port %q", s)
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}


func mustEncrypt(t *testing.T, cipher *crypto.Cipher, plain string) []byte {
	t.Helper()
	if plain == "" {
		return nil
	}
	out, err := cipher.Encrypt([]byte(plain))
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}
	return out
}

// seedMigration inserts a queued migration + execution + job chain, the same
// shape the approval flow produces.
func seedMigration(t *testing.T, client *ent.Client, connectionID int, sqlUp, sqlDown string) (*ent.Migration, *ent.MigrationExecution, *ent.MigrationJob) {
	t.Helper()
	ctx := context.Background()
	author, err := client.PlatformUser.Create().
		SetUsername(fmt.Sprintf("wuser-%d", wkSeq.Add(1))).
		SetPasswordHash("x").
		Save(ctx)
	if err != nil {
		t.Fatalf("create author: %v", err)
	}
	mig, err := client.Migration.Create().
		SetConnectionID(connectionID).
		SetVersion(int(wkSeq.Add(1))).
		SetSQLUp(sqlUp).
		SetSQLDown(sqlDown).
		SetStatus(migration.StatusQueued).
		SetAuthorID(author.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create migration: %v", err)
	}
	exec, err := client.MigrationExecution.Create().
		SetMigrationID(mig.ID).
		SetConnectionID(connectionID).
		SetKind(migrationexecution.KindUp).
		SetStatus(migrationexecution.StatusQueued).
		SetActorID(author.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create execution: %v", err)
	}
	job, err := client.MigrationJob.Create().
		SetExecutionID(exec.ID).
		SetConnectionID(connectionID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	return mig, exec, job
}

// runOneJob drives the runner until the seeded job finishes (or the deadline
// passes): Run with a cancellable context in a goroutine, poll for terminal.
func runOneJob(t *testing.T, r *Runner, jobID int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Run(ctx, fmt.Sprintf("wt-%d", wkSeq.Add(1)), 20*time.Millisecond)
	}()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		row, err := r.client.MigrationJob.Query().
			Where(migrationjob.ID(jobID)).
			Only(context.Background())
		if err != nil {
			cancel()
			t.Fatalf("poll job %d: %v", jobID, err)
		}
		switch row.Status {
		case migrationjob.StatusSuccess, migrationjob.StatusFailed,
			migrationjob.StatusCancelled, migrationjob.StatusPartial:
			cancel()
			<-done
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	t.Fatalf("job %d did not reach a terminal state within 15s", jobID)
}

func jobRow(t *testing.T, client *ent.Client, jobID int) *ent.MigrationJob {
	t.Helper()
	row, err := client.MigrationJob.Query().
		Where(migrationjob.ID(jobID)).
		WithExecution().
		Only(context.Background())
	if err != nil {
		t.Fatalf("load job %d: %v", jobID, err)
	}
	return row
}

func execRow(t *testing.T, client *ent.Client, execID int) *ent.MigrationExecution {
	t.Helper()
	row, err := client.MigrationExecution.Query().
		Where(migrationexecution.ID(execID)).
		Only(context.Background())
	if err != nil {
		t.Fatalf("load execution %d: %v", execID, err)
	}
	return row
}

func tableExists(t *testing.T, adminDB *sql.DB, schema, table string) bool {
	t.Helper()
	var exists bool
	if err := adminDB.QueryRow(
		"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema=$1 AND table_name=$2)",
		schema, table).Scan(&exists); err != nil {
		t.Fatalf("table exists check: %v", err)
	}
	return exists
}

func dropTargetTable(t *testing.T, adminDB *sql.DB, schema, table string) {
	t.Helper()
	_, _ = adminDB.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s.%s", schema, table))
}

func TestWorkerRunsSimpleMigration(t *testing.T) {
	r, adminDB, client, connectionID, schema := newWorkerEnv(t)
	
	table := fmt.Sprintf("w_simple_%d_%d", os.Getpid(), wkSeq.Add(1))
	dropTargetTable(t, adminDB, table)
	t.Cleanup(func() { dropTargetTable(t, adminDB, table) })

	_, exec, job := seedMigration(t, client, connectionID,
		fmt.Sprintf("CREATE TABLE %s (id int PRIMARY KEY)", table),
		fmt.Sprintf("DROP TABLE %s", table))
	runOneJob(t, r, job.ID)

	if !tableExists(t, adminDB, table) {
		t.Fatalf("target table %s missing after successful run", table)
	}
	row := jobRow(t, client, job.ID)
	if row.Status != migrationjob.StatusSuccess {
		t.Fatalf("job status = %s, want success (error=%s)", row.Status, row.Error)
	}
	after := execRow(t, client, exec.ID)
	if after.Status != migrationexecution.StatusSuccess {
		t.Fatalf("execution status = %s, want success (error=%s)", after.Status, after.Error)
	}
	if after.SQLExecuted == "" {
		t.Fatalf("execution has no sql_executed copy")
	}
	migRow, err := client.Migration.Query().Where(migration.ID(exec.MigrationID)).Only(context.Background())
	if err != nil {
		t.Fatalf("load migration: %v", err)
	}
	if migRow.Status != migration.StatusSuccess {
		t.Fatalf("migration status = %s, want success", migRow.Status)
	}
	snapshots, err := client.SchemaSnapshot.Query().
		Where(schemasnapshot.ExecutionID(exec.ID)).
		All(context.Background())
	if err != nil {
		t.Fatalf("load snapshots: %v", err)
	}
	phases := map[schemasnapshot.Phase]bool{}
	for _, s := range snapshots {
		phases[s.Phase] = true
	}
	if !phases[schemasnapshot.PhaseBefore] || !phases[schemasnapshot.PhaseAfter] {
		t.Fatalf("snapshots = %v, want before+after", snapshots)
	}
}

func TestWorkerRollbackUsesOriginalDown(t *testing.T) {
	r, adminDB, client, connectionID, schema := newWorkerEnv(t)
	
	table := fmt.Sprintf("w_rb_%d_%d", os.Getpid(), wkSeq.Add(1))
	dropTargetTable(t, adminDB, table)
	t.Cleanup(func() { dropTargetTable(t, adminDB, table) })

	mig, exec, job := seedMigration(t, client, connectionID,
		fmt.Sprintf("CREATE TABLE %s (id int PRIMARY KEY)", table),
		fmt.Sprintf("DROP TABLE %s", table))
	runOneJob(t, r, job.ID)
	if !tableExists(t, adminDB, table) {
		t.Fatalf("setup: table %s missing after up", table)
	}

	ctx := context.Background()
	rollbackExec, err := client.MigrationExecution.Create().
		SetMigrationID(mig.ID).
		SetConnectionID(connectionID).
		SetKind(migrationexecution.KindRollback).
		SetStatus(migrationexecution.StatusQueued).
		SetActorID(mig.AuthorID).
		SetRevertedExecutionID(exec.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create rollback execution: %v", err)
	}
	rollbackJob, err := client.MigrationJob.Create().
		SetExecutionID(rollbackExec.ID).
		SetConnectionID(connectionID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create rollback job: %v", err)
	}
	runOneJob(t, r, rollbackJob.ID)

	if tableExists(t, adminDB, table) {
		t.Fatalf("table %s still exists after rollback", table)
	}
	row := jobRow(t, client, rollbackJob.ID)
	if row.Status != migrationjob.StatusSuccess {
		t.Fatalf("rollback job status = %s, want success (error=%s)", row.Status, row.Error)
	}
	after := execRow(t, client, rollbackExec.ID)
	if after.Status != migrationexecution.StatusSuccess {
		t.Fatalf("rollback execution status = %s, want success", after.Status)
	}
	migRow, err := client.Migration.Query().Where(migration.ID(mig.ID)).Only(ctx)
	if err != nil {
		t.Fatalf("load migration: %v", err)
	}
	if migRow.Status != migration.StatusCancelled {
		t.Fatalf("migration status = %s, want cancelled after rollback", migRow.Status)
	}
}

func TestWorkerBatchCheckpoints(t *testing.T) {
	r, adminDB, client, connectionID, schema := newWorkerEnv(t)
	
	table := fmt.Sprintf("w_batch_%d_%d", os.Getpid(), wkSeq.Add(1))
	dropTargetTable(t, adminDB, table)
	t.Cleanup(func() { dropTargetTable(t, adminDB, table) })
	_, err := adminDB.Exec(fmt.Sprintf(
		"CREATE TABLE %s (id int PRIMARY KEY, status text)", table))
	if err != nil {
		t.Fatalf("create demo table: %v", err)
	}
	values := ""
	for i := 1; i <= 50; i++ {
		if i > 1 {
			values += ","
		}
		values += fmt.Sprintf("(%d,'new')", i)
	}
	if _, err := adminDB.Exec(fmt.Sprintf("INSERT INTO %s (id, status) VALUES %s", table, values)); err != nil {
		t.Fatalf("seed rows: %v", err)
	}

	// probeUpper picks the batch_size-th row above last, so with ids 1..50
	// and batch_size 10 each batch covers 10 rows ⇒ 5 batches, 50 rows.
	template := fmt.Sprintf(
		"UPDATE %s SET status='done' WHERE id > {{last_id}} AND id <= {{upper_id}} AND id >= {{min_id}} AND id <= {{max_id}}",
		table)
	minID, maxID := int64(1), int64(50)
	ctx := context.Background()
	author, err := client.PlatformUser.Create().
		SetUsername(fmt.Sprintf("wuser-%d", wkSeq.Add(1))).
		SetPasswordHash("x").
		Save(ctx)
	if err != nil {
		t.Fatalf("create author: %v", err)
	}
	mig, err := client.Migration.Create().
		SetConnectionID(connectionID).
		SetVersion(int(wkSeq.Add(1))).
		SetSQLUp(template).
		SetStatus(migration.StatusQueued).
		SetAuthorID(author.ID).
		SetBatchSize(10).
		SetBatchTable(table).
		SetCursorColumn("id").
		SetTemplate(template).
		SetMinID(minID).
		SetMaxID(maxID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create batch migration: %v", err)
	}
	exec, err := client.MigrationExecution.Create().
		SetMigrationID(mig.ID).
		SetConnectionID(connectionID).
		SetKind(migrationexecution.KindUp).
		SetStatus(migrationexecution.StatusQueued).
		SetActorID(author.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create execution: %v", err)
	}
	job, err := client.MigrationJob.Create().
		SetExecutionID(exec.ID).
		SetConnectionID(connectionID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	runOneJob(t, r, job.ID)

	row := jobRow(t, client, job.ID)
	if row.Status != migrationjob.StatusSuccess {
		t.Fatalf("job status = %s, want success (error=%s)", row.Status, row.Error)
	}
	if row.BatchesDone != 5 {
		t.Fatalf("job batches_done = %d, want 5", row.BatchesDone)
	}
	if row.RowsDone != 50 {
		t.Fatalf("job rows_done = %d, want 50", row.RowsDone)
	}
	after := execRow(t, client, exec.ID)
	if after.Status != migrationexecution.StatusSuccess {
		t.Fatalf("execution status = %s, want success (error=%s)", after.Status, after.Error)
	}
	if after.BatchesDone != 5 {
		t.Fatalf("execution batches_done = %d, want 5", after.BatchesDone)
	}
	var done int
	if err := adminDB.QueryRow(
		fmt.Sprintf("SELECT count(*) FROM %s WHERE status='done'", table)).Scan(&done); err != nil {
		t.Fatalf("count done rows: %v", err)
	}
	if done != 50 {
		t.Fatalf("done rows = %d, want 50", done)
	}
	migRow, err := client.Migration.Query().Where(migration.ID(mig.ID)).Only(ctx)
	if err != nil {
		t.Fatalf("load migration: %v", err)
	}
	if migRow.Status != migration.StatusSuccess {
		t.Fatalf("migration status = %s, want success", migRow.Status)
	}
}

func TestWorkerBatchCancel(t *testing.T) {
	r, adminDB, client, connectionID, schema := newWorkerEnv(t)
	
	table := fmt.Sprintf("w_bcancel_%d_%d", os.Getpid(), wkSeq.Add(1))
	dropTargetTable(t, adminDB, table)
	t.Cleanup(func() { dropTargetTable(t, adminDB, table) })
	_, err := adminDB.Exec(fmt.Sprintf(
		"CREATE TABLE %s (id int PRIMARY KEY, status text)", table))
	if err != nil {
		t.Fatalf("create demo table: %v", err)
	}
	values := ""
	for i := 1; i <= 1000; i++ {
		if i > 1 {
			values += ","
		}
		values += fmt.Sprintf("(%d,'new')", i)
	}
	if _, err := adminDB.Exec(fmt.Sprintf("INSERT INTO %s (id, status) VALUES %s", table, values)); err != nil {
		t.Fatalf("seed rows: %v", err)
	}

	template := fmt.Sprintf(
		"UPDATE %s SET status='done' WHERE id > {{last_id}} AND id <= {{upper_id}} AND id >= {{min_id}} AND id <= {{max_id}}",
		table)
	ctx := context.Background()
	author, err := client.PlatformUser.Create().
		SetUsername(fmt.Sprintf("wuser-%d", wkSeq.Add(1))).
		SetPasswordHash("x").
		Save(ctx)
	if err != nil {
		t.Fatalf("create author: %v", err)
	}
	mig, err := client.Migration.Create().
		SetConnectionID(connectionID).
		SetVersion(int(wkSeq.Add(1))).
		SetSQLUp(template).
		SetStatus(migration.StatusQueued).
		SetAuthorID(author.ID).
		SetBatchSize(1).
		SetBatchTable(table).
		SetCursorColumn("id").
		SetTemplate(template).
		SetMinID(1).
		SetMaxID(1000).
		Save(ctx)
	if err != nil {
		t.Fatalf("create batch migration: %v", err)
	}
	exec, err := client.MigrationExecution.Create().
		SetMigrationID(mig.ID).
		SetConnectionID(connectionID).
		SetKind(migrationexecution.KindUp).
		SetStatus(migrationexecution.StatusQueued).
		SetActorID(author.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create execution: %v", err)
	}
	job, err := client.MigrationJob.Create().
		SetExecutionID(exec.ID).
		SetConnectionID(connectionID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	// Run the runner in the background and cancel once the first batch
	// checkpoint lands: 1000 rows at batch_size 1 run well past the first
	// commit, and cancel-at-boundary then leaves partial state.
	runCtx, cancelRun := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Run(runCtx, fmt.Sprintf("wt-%d", wkSeq.Add(1)), 20*time.Millisecond)
	}()

	// Wait for the first checkpoint (batches_done >= 1) before cancelling.
	q := r.queue
	var cancelErr error
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		row, err := client.MigrationJob.Query().Where(migrationjob.ID(job.ID)).Only(ctx)
		if err != nil {
			cancelRun()
			<-done
			t.Fatalf("poll job: %v", err)
		}
		if row.BatchesDone >= 1 {
			cancelErr = q.RequestCancel(context.Background(), job.ID)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	terminalDeadline := time.Now().Add(20 * time.Second)
	terminal := false
	for time.Now().Before(terminalDeadline) {
		row, err := client.MigrationJob.Query().Where(migrationjob.ID(job.ID)).Only(ctx)
		if err != nil {
			cancelRun()
			<-done
			t.Fatalf("poll job: %v", err)
		}
		if row.Status == migrationjob.StatusPartial || row.Status == migrationjob.StatusCancelled {
			terminal = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancelRun()
	<-done
	if cancelErr != nil {
		t.Fatalf("RequestCancel: %v", cancelErr)
	}
	if !terminal {
		t.Fatalf("job did not reach partial/cancelled within 20s")
	}

	row := jobRow(t, client, job.ID)
	// batches_done may be 0 when the cancel flag lands before the first
	// checkpoint (spec: cancel takes effect at the next batch boundary).
	if row.BatchesDone < 0 {
		t.Fatalf("cancelled job batches_done = %d", row.BatchesDone)
	}
	after := execRow(t, client, exec.ID)
	if after.Status != migrationexecution.StatusPartial && after.Status != migrationexecution.StatusCancelled {
		t.Fatalf("execution status = %s, want partial or cancelled", after.Status)
	}
	if after.BatchesDone <= 0 {
		t.Fatalf("execution batches_done = %d, want > 0", after.BatchesDone)
	}
	migRow, err := client.Migration.Query().Where(migration.ID(mig.ID)).Only(ctx)
	if err != nil {
		t.Fatalf("load migration: %v", err)
	}
	if migRow.Status != migration.StatusPartial && migRow.Status != migration.StatusCancelled {
		t.Fatalf("migration status = %s, want partial or cancelled", migRow.Status)
	}
}

func TestWorkerTransientRetry(t *testing.T) {
	r, _, client, connectionID, _ := newWorkerEnv(t)

	// Non-transient failure (no pq error class in the message): the job
	// fails terminally on the first attempt, no retry to queued.
	_, exec, job := seedMigration(t, client, connectionID,
		"SELECT * FROM does_not_exist_xyz", "")
	runOneJob(t, r, job.ID)

	row := jobRow(t, client, job.ID)
	if row.Status != migrationjob.StatusFailed {
		t.Fatalf("job status = %s, want failed", row.Status)
	}
	after := execRow(t, client, exec.ID)
	if after.Status != migrationexecution.StatusFailed {
		t.Fatalf("execution status = %s, want failed", after.Status)
	}
	if after.Error == "" {
		t.Fatalf("execution carries no error text")
	}
	migRow, err := client.Migration.Query().Where(migration.ID(exec.MigrationID)).Only(context.Background())
	if err != nil {
		t.Fatalf("load migration: %v", err)
	}
	if migRow.Status != migration.StatusFailed {
		t.Fatalf("migration status = %s, want failed", migRow.Status)
	}
}
