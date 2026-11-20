# Handoff: IF3110 Milestone 2 — Orpbal async migration implementation

## Current working directory
`/home/liro/dev/personal/school/WBD/Orpbal` (repo root). Branch `main`, 93 modified/added files uncommitted, nothing staged.

## Where things stand (settled via grilling session; plan ratified)
All 17 M2 tasks decided, ~14 done. Backend builds green (`go build ./...`), `go vet ./...` clean, `go test ./...` → 11 packages OK. Frontend: `bun run check-types`, vitest (11 tests), build all pass.

## Completed work
- **Contract**: `apps/server/api/openapi.yaml` extended with M2 endpoints (submit/rollback/forward/revert-batches → job ID; job status polling; approvals; history with `hide_rollback`; cancel). Regenerated `internal/api/openapi.gen.go` (ServerInterface has all new methods).
- **Schema**: `ent/schema/migration.go` rewritten (sql_up/sql_down/irreversible/reason/batch config) + new `migration_execution.go`, `migration_job.go`, `migration_approval.go`; `group_membership.go` gained `role` enum (member/leader); `schema_snapshot.go` keyed on `execution_id`. `go generate ./ent` passes.
- **Queue** (`internal/queue/`): SKIP LOCKED claiming, advisory `pg_try_advisory_lock`, heartbeat, checkpoint, retry (max 3, transient classes), RequestCancel/IsCancelled, `ErrNotClaimed/ErrNotOwner/ErrTerminal`. Unit tests pass against test postgres (`TEST_DATABASE_URL=postgres://test:test@127.0.0.1:54329/test?sslmode=disable`).
- **Worker** (`cmd/worker/`, `internal/worker/`): separate binary, claim → advisory lock → execute → commit; batch mode with cursor probe (`probeUpper` now bounded by batch_size via LIMIT subselect); per-batch checkpoint; rollback uses original sql_down; tracing Init("orpbal-worker"). `docker-compose.yml` + `Dockerfile` updated to build/run `worker` service.
- **Service+Handlers** (`internal/service/migration.go`, `internal/handler/migration2_handler.go` + `migration_handler.go` rewrite, `migration_diff.go` rewrite): approve/reject with leader gating (all leaders required), auto-enqueue job on last approval, traceparent on job row, hide-rollback view filter. Builds green.
- **Frontend** (`apps/web/`): `migrations.ts`, `MigrationsPage.tsx`, `JobDetailPage.tsx`, `ApprovalsPage.tsx`, routes in `App.tsx`, nav in `AppLayout.tsx`. Typecheck/build/tests pass.
- **Infra/bonus**: `scripts/native/task-5.sh` seed script (2M rows, timing probe), `k6/stress.js` (ramp/peak/down, thresholds), `.github/workflows/codeql.yml`, README sections (architecture, worker env vars, k6, CodeQL).
- **Tracing**: `internal/tracing/tracing.go` (stdout OTel exporter, traceparent on job rows, zerolog trace_id/span_id correlation).

## Immediate remaining work (next agent)
1. **Fix worker_test.go compile error** — `dropTargetTable` signature changed to 4 args `(t, adminDB, schema, table)` but call sites at ~lines 281, 329, 385, 491 still pass 3 args. `newWorkerEnv` now returns 5th value `schema`; all call sites destructure it already. Fix: `dropTargetTable(t, adminDB, schema, table)` and `t.Cleanup(func() { dropTargetTable(t, adminDB, schema, table) })`.
2. **Run worker tests green**: `export TEST_DATABASE_URL=postgres://test:test@127.0.0.1:54329/test?sslmode=disable` (test postgres at `docker compose -f docker-compose.test.yml up -d postgres-test`) then `cd apps/server && go test ./internal/worker/ -count=1`.
3. **Full suite green**: `go test ./...` (11 packages) and frontend gates; then `git add -A && git commit` with a phase-naming message.

## Key context / gotchas
- Worker `runBatch` probeUpper now limits batch by batch_size (LIMIT subselect + MAX cursor) so batch_size is honored.
- Drop table names in worker tests were made process-unique (`os.Getpid()`) to avoid collisions between concurrent test-binaries hitting the shared test DB's `public` schema.
- `dsn.Config`/`postgresDSN` does NOT carry `search_path` — target DSN connects to default schema; tables are created `public.<name>` in worker tests.
- `internal/tracing/tracing.go` exists and builds; do not recreate it.
- `k6` not installed locally — stress script is authored but not executed.

## Suggested skills for next agent
- `tdd` — for finishing worker/service/handler integration tests red-green-refactor.
- `setup-pre-commit` — optional, repo has CI but no husky; not required.
- No other skills needed; work is implementation + verification + commit.

## Artifacts (reference, do not duplicate)
- `local://M2-WORKER-CONTRACT.md` — settled design contract (state machines, queue API, worker contract, handler signatures).
- `local://M2-TRACING-NOTES.md` — tracing wiring points.
- `docs/` — created but empty; handoff report goes here as markdown.
- `k6/stress.js`, `scripts/native/task-5.sh`, `.github/workflows/codeql.yml` — bonus artifacts.
