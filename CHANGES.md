# Perubahan Kepatuhan (Audit M1 + M2, 27 November 2026)

Dokumen ini merangkum perubahan kode yang dibuat untuk menutup gap requirement **wajib** Milestone 1 dan Milestone 2 IF3110 sebagaimana temuan audit kepatuhan (lihat `docs/` dan README). Semua perubahan diverifikasi: `go vet ./...` bersih, `go test -count=1 ./...` hijau penuh (integration test dengan PostgreSQL 17 lokal), `tsc --noEmit && vite build` hijau, `vitest` 11/11 hijau.

## Wajib M1

### 1. Urutan eksekusi versi migration dikunci (linear history)
- `service/migration.go`: `Approve` menolak (rollback transaksi, error `409 version_order`) final approval migration **up** bila masih ada versi lebih kecil pada database yang sama dengan status draft/pending_review/queued/running/partial. Rollback/forward/retry/batch_revert dikecualikan; versi gagal terminal tidak memblokir (kegagalannya sudah entri permanen riwayat).
- Test: `TestMigrationLifecycle` kini mengassert penolakan out-of-order lalu keberhasilan setelah versi awal diselesaikan.

### 2. Delete koneksi database
- `openapi.yaml`: `DELETE /api/admin/databases/{id}` (`deleteDatabaseConnection`).
- `handler/admin_connection_lifecycle.go`: hapus koneksi + grant dalam satu transaksi; **ditolak 409** bila masih ada migration pada target (riwayat linear tidak boleh kehilangan targetnya); pool koneksi di-invalidate.
- UI: tombol Delete di halaman Admin → Databases dengan konfirmasi.
- Test: `TestDatabaseConnectionLifecycle`.

### 3. Endpoint uji koneksi mandiri
- `openapi.yaml`: `POST /api/admin/databases/{id}/test` (`testDatabaseConnection`), respons `ConnectionTestResult`.
- Handler ping koneksi tersimpan (termasuk koneksi **disabled**, via `Manager.OpenByID(requireEnabled=false)`); kegagalan → `502 target_unreachable` dengan pesan **teredaksi**; kredensial tidak pernah keluar.
- UI: tombol "Test connection" di halaman Admin → Databases.
- Test: `TestDatabaseConnectionLifecycle`, `TestDatabaseConnectionTestUnreachable`.

## Wajib M2

### 4. Riwayat attempt append-only (revisi spesifikasi 13 Nov 2026)
- Entitas baru `MigrationAttempt` (`ent/schema/migration_attempt.go`, tabel `migration_attempts`): satu baris **imutabel per attempt** — execution_id, migration_id, nomor attempt, kind, status (running/success/failed/partial/cancelled), salinan `sql_executed`, error, actor, worker, batch cursor/progress, started/finished.
- `worker/worker.go`: setiap claim menulis baris attempt; setiap jalur akhir (sukses, gagal, partial/cancelled, ditinggalkan karena checkpoint gagal) menutup barisnya **tepat sekali**; progress batch diperbarui pada baris attempt yang berjalan.
- `queue/queue.go`: `FinishFailed` **tidak lagi meng-reset** baris execution ke `queued` saat retry transient — execution tetap `failed` (attempt history tidak ditimpa); job kembali `queued`.
- API: `execution.attempts` (array `MigrationAttempt`) pada history migration; UI MigrationsPage menampilkan attempt di bawah tiap eksekusi.
- Test: `TestAttemptHistoryAppendOnly`, `TestWorkerTransientRetryAttemptRows`.

### 5. Tracing API → queue → worker tersambung
- `cmd/server/main.go`: `tracing.Init("orpbal-api")` + middleware `tracing.GinMiddleware` (span `HTTP <method> <route>` per request, ekstraksi `traceparent` W3C masuk, status/durasi pada span, request log dengan `trace_id`/`span_id`).
- Konsekuensinya `traceparent` job (ditulis saat approval) kini terisi dari span request, dan worker melanjutkan trace yang sama — rantai span API → queue → worker → lock → batch utuh.

### 6. Activity logging: semua SQL + atribut wajib + redaksi
- Paket baru `internal/sqllog`: logger audit via context, `Redact` (DSN password, literal `password/token/secret`, `PASSWORD('x')`; SQL dipotong 200 char), `SQLState` (lib/pq + go-sql-driver).
- `internal/target` (`Query`, `ExecuteArgs`, `Explain`): log `sql_query_start/done`, `sql_exec_start`, `sql_commit`, `sql_rollback` (terpisah), `sql_explain_done` dengan waktu, level, event, actor, nama database, engine, status, durasi, rows, error+SQLSTATE, sql (redacted). Worker menyuntikkan korelasi penuh (job/execution/attempt/migration/batch) ke logger sehingga log SQL level target ikut terkorelasi.
- `cmd/server` + `cmd/worker`: **JSON default di semua mode** (`LOG_FORMAT=console` opsional); middleware API menempelkan actor ke logger konteks.
- Middleware API baru menautkan `sqllog` logger (actor + trace) ke konteks request.
- Test: `internal/sqllog` (redaksi, SQLSTATE, batas panjang).

### 7. Gate partial antar-migration
- `queue/queue.go` `claimSQL`: selama ada job `partial` pada sebuah koneksi, hanya job `retry`/`batch_revert` (jalur keluar yang disetujui) yang bisa di-claim — migration baru up/forward menunggu kepastian status.
- Test: `TestClaimBlocksNewMigrationWhilePartial` (blokir → retry lolos → resolved terbuka lagi).

### 8. Pembersihan workflow
- Branch UI mati untuk status `approved` dihapus dari MigrationsPage (service tidak pernah menetapkan status tersebut; transisi langsung approved→queued dalam satu transaksi).

## Dokumentasi
- README: seksi baru "Activity Logging & Tracing (JSON terstruktur)" (tabel atribut + contoh baris log JSON + contoh span), redaksi log di "Keamanan Data", keputusan desain diperbarui (attempt ledger, version-order gate, partial gate, batch revert deviasi diakui eksplisit, kebijakan delete koneksi, reset approval per siklus), angka dataset besar terukur (500.000 baris: 1,21 s unbatched vs 3,64 s batched; 27 Nov 2026), `LOG_FORMAT` di tabel env, rute admin baru.
- `.env.example`: `LOG_LEVEL` diganti `LOG_FORMAT`.

## Observability (Prometheus + Grafana, 27 November 2026 — sesi kedua)

Implementasi rekomendasi bonus audit "observability", digabung di atas perubahan wajib M1+M2:

### Metrik instrumentasi (`internal/metrics`, dependensi `prometheus/client_golang v1.20.5`)
- API (`middleware gin`): `orpbal_http_requests_total{route,method,status}` + histogram `orpbal_http_request_duration_seconds` (label route template, kardinalitas terkendali).
- Queue (`internal/queue`): `orpbal_migration_jobs_claimed_total{worker}`, `orpbal_migration_job_outcomes_total{status}` untuk success/failed/retry (FinishSuccess/FinishFailed), cancelled (RequestCancel jalur queued).
- Worker (`internal/worker`): outcomes cancelled/partial lewat `finishJobStatus`, `orpbal_migration_batches_committed_total`, `orpbal_migration_batch_rows_total` (per batch commit), `orpbal_migration_attempts_total{kind}` (baris attempt ledger).
- Server: gauge `orpbal_migration_jobs{status}` di-refresh tiap 15 s dari `migration_jobs` (paket `internal/database` kini membagikan satu pool `*sql.DB` antara ent dan konsumen SQL langsung via `SQLDB()`).
- Endpoint: `/metrics` di API (`cmd/server`) dan server HTTP kecil di worker (`WORKER_METRICS_ADDR`, default `:9102`, graceful shutdown).

### Stack deploy (`deploy/`, `docker-compose.yml`)
- `prometheus` (v2.53.3): scrape `app:8080` + `worker:9102` tiap 15 s, retensi 7 hari, alert rules `OrpbalMigrationJobStuck` / `OrpbalMigrationQueueBacklog` / `OrpbalMigrationFailures` / `OrpbalHighHTTPErrorRate`.
- `grafana` (v11.1.0, port host 3001 karena web di 3000): provisioning datasource + dashboard **Orpbal Overview** (7 panel: request rate, p95, 5xx, job per status, claims & outcomes, throughput batch, attempt per kind).
- Unit test `internal/metrics/metrics_test.go` (middleware route metrics, gauge reset, counter outcomes).

### Verifikasi sesi kedua
- `go mod tidy`, `go vet ./...`, `go test -count=1 ./...` penuh dengan PostgreSQL 17 lokal — seluruh paket ok (lihat log verifikasi).
- Validasi sintaks: `docker-compose.yml`, semua YAML `deploy/`, dan JSON dashboard divalidasi dengan parser YAML/JSON.

## Bukti verifikasi
- `go vet ./internal/... ./cmd/...` — bersih.
- `go test -count=1 ./...` dengan `TEST_DATABASE_URL` (PostgreSQL 17.12 lokal): **seluruh paket ok** (access, handler, queue, schema/build, security/*, service, sqllog, target, worker).
- `tsc --noEmit && vite build` + `vitest run`: hijau (11 test).
