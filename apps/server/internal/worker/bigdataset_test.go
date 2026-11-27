package worker

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"Orpbal/ent/migration"
	"Orpbal/ent/migrationexecution"
	"Orpbal/ent/migrationjob"
)

// TestWorkerLargeDatasetBatch verifies the spec's mandatory pengujian
// ketentuan: a dataset big enough that ONE unbatched UPDATE takes > 1s,
// then the SAME dataset migrated per batch. Row count is calibrated at test
// start (doubling until the unbatched UPDATE exceeds 1s, capped at 8M rows
// / ~5 calibration rounds so slow environments still finish).
func TestWorkerLargeDatasetBatch(t *testing.T) {
	r, adminDB, client, connectionID, _ := newWorkerEnv(t)

	table := fmt.Sprintf("w_big_%d_%d", os.Getpid(), wkSeq.Add(1))
	dropTargetTable(t, adminDB, "public", table)
	t.Cleanup(func() { dropTargetTable(t, adminDB, "public", table) })

	if _, err := adminDB.Exec(fmt.Sprintf(
		"CREATE TABLE public.%s (id int PRIMARY KEY, status text)", table)); err != nil {
		t.Fatalf("create big table: %v", err)
	}

	// Calibrate: seed N rows, time one whole-table UPDATE, double until >1s.
	// Each round drops and reseeds so the probe UPDATE leaves no residue.
	unbatched := time.Duration(0)
	rows := 0
	for n := int64(250000); n <= 8_000_000; n *= 2 {
		if _, err := adminDB.Exec(fmt.Sprintf("TRUNCATE public.%s", table)); err != nil {
			t.Fatalf("truncate: %v", err)
		}
		if _, err := adminDB.Exec(fmt.Sprintf(
			"INSERT INTO public.%s (id, status) SELECT g, 'new' FROM generate_series(1, %d) g", table, n)); err != nil {
			t.Fatalf("seed %d rows: %v", n, err)
		}
		start := time.Now()
		if _, err := adminDB.Exec(fmt.Sprintf("UPDATE public.%s SET status='slow'", table)); err != nil {
			t.Fatalf("unbatched UPDATE (%d rows): %v", n, err)
		}
		unbatched = time.Since(start)
		rows = int(n)
		t.Logf("calibration: %d rows, unbatched UPDATE took %s", rows, unbatched)
		if unbatched > time.Second {
			break
		}
	}
	if unbatched <= time.Second {
		t.Fatalf("could not reach >1s unbatched UPDATE within cap (last: %d rows in %s)",
			rows, unbatched)
	}
	t.Logf("dataset: %d rows, single unbatched UPDATE = %s (>1s requirement met)", rows, unbatched)

	// Same dataset, same UPDATE, per batch through the worker.
	if _, err := adminDB.Exec(fmt.Sprintf("UPDATE public.%s SET status='new'", table)); err != nil {
		t.Fatalf("reset statuses: %v", err)
	}
	template := fmt.Sprintf(
		"UPDATE public.%s SET status='done' WHERE id > {{last_id}} AND id <= {{upper_id}} AND id >= {{min_id}} AND id <= {{max_id}}",
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
		SetBatchSize(10000).
		SetBatchTable(table).
		SetCursorColumn("id").
		SetTemplate(template).
		SetMinID(1).
		SetMaxID(int64(rows)).
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

	batchStart := time.Now()
	// 250k rows across 25 batches take ~10–14s; under load the whole run can
	// stretch well past 15s, so give the poll loop 180s.
	runOneJobDeadline(t, r, job.ID, 180*time.Second)
	batched := time.Since(batchStart)

	row := jobRow(t, client, job.ID)
	if row.Status != migrationjob.StatusSuccess {
		t.Fatalf("job status = %s, want success (error=%s)", row.Status, row.Error)
	}
	if row.RowsDone != int64(rows) {
		t.Fatalf("job rows_done = %d, want %d", row.RowsDone, rows)
	}
	var done int
	if err := adminDB.QueryRow(
		fmt.Sprintf("SELECT count(*) FROM public.%s WHERE status='done'", table)).Scan(&done); err != nil {
		t.Fatalf("count done rows: %v", err)
	}
	if done != rows {
		t.Fatalf("done rows = %d, want %d", done, rows)
	}

	t.Logf("RESULT: rows=%d unbatched=%s batched(worker, batch_size=10000, %d batches)=%s",
		rows, unbatched, row.BatchesDone, batched)

	var _ *sql.DB = adminDB // keep import stable if helpers change
}
