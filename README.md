
# Orpbal


---

## Overview

Orpbal adalah platform operasi database mandiri (self-hosted) tingkat enterprise. Aplikasi ini menggabungkan pemodelan skema visual, eksekusi query, pengelolaan data tabel, dan pelacakan migrasi ke dalam satu dashboard web terpadu dengan backend Go berkecepatan tinggi.


---

## Key Features

### Visualisasi Skema & Diagram ER Interaktif
Lihat relasi antar tabel secara visual (drag-and-drop) dan ubah struktur skema langsung dari kanvas untuk menghasilkan file migrasi DDL otomatis.

### Explorer & Editor Data Tabel
Lihat isi data tabel dengan pagination serta lakukan tambah, edit, dan hapus baris secara aman (wajib memiliki primary key).

### Konsol Query SQL 
Editor SQL berbasis CodeMirror dengan pembatasan keamanan transaksi read-only dan limit hasil maksimal 10.000 baris agar tidak membebani memori.

### Pelacakan Migrasi & Schema Diff
Catatan riwayat migrasi bertahap yang otomatis mengambil snapshot sebelum dan sesudah eksekusi, sehingga diff skema dapat dibandingkan dengan jelas.

### Analisis Query Plan (EXPLAIN / EXPLAIN ANALYZE)
Rencanakan query dari SQL runner atau migration yang masih `pending` sebelum dieksekusi. PostgreSQL menampilkan pohon plan terstruktur berisi strategi scan dan join, index yang dipakai, estimated cost/rows, serta actual time dan row count saat ANALYZE dijalankan. `EXPLAIN ANALYZE` dieksekusi di dalam transaksi read-only sehingga perubahan data tetap ditolak dan hasil eksekusi dibuang. Target MySQL menampilkan keluaran EXPLAIN tabular.


## Repository Structure

```
Orpbal/
├── .github/
│   └── workflows/
│       ├── ci.yml               
│       └── codeql.yml           
├── apps/
│   ├── server/                  
│   │   ├── api/                
│   │   ├── cmd/server/          
│   │   ├── ent/                 
│   │   │   └── schema/          
│   │   ├── internal/
│   │   │   ├── access/          
│   │   │   ├── api/             
│   │   │   ├── auth/            
│   │   │   ├── bootstrap/       
│   │   │   ├── config/         
│   │   │   ├── database/       
│   │   │   ├── handler/         
│   │   │   ├── security/        
│   │   │   └── target/         
│   │   ├── Dockerfile        
│   │   ├── go.mod              
│   │   └── oapi-codegen.yaml   
│   └── web/                     
│       ├── public/              
│       ├── src/
│       │   ├── auth/     
│       │   ├── components/
│       │   ├── layouts/  
│       │   ├── lib/
│       │   ├── pages/ 
│       │   │   ├── admin/
│       │   │   ├── databases/ 
│       │   │   ├── login.tsx  
│       │   │   └── logout.tsx
│       │   └── styles/  
│       ├── Dockerfile.nginx
│       ├── nginx.conf
│       └── package.json
├── packages/
│   ├── config/  
│   └── db/                     
├── scripts/
│   ├── native/                  
│   └── test-integration.sh    
├── docker-compose.yml           
├── docker-compose.test.yml      
├── package.json
└── README.md                    
```

---

## Tech Stack

| Domain | Technology | Description |
|---|---|---|
| Frontend Framework |	React 19 + Vite 8 |	Single-Page Application (SPA) reaktif; seluruh state dan alur hidup sisi klien |
| Routing |	React Router 7 |	SPA routing dengan URL yang tetap (`/login`, `/databases`, `/admin/*`) |
| Diagram Engine |	@xyflow/react + Dagre |	Editor graf visual interaktif dengan tata letak otomatis untuk diagram relasional
| SQL Editor |	CodeMirror 6 (@uiw/react-codemirror)	| Editor web canggih dengan penyorotan sintaksis SQL, nomor baris, dan pelengkapan otomatis
| Styling |	Tailwind CSS v4	| Styling modern responsif (mobile dan desktop)
| Penyajian (Docker) |	nginx |	Melayani `dist/` hasil build, fallback SPA routing, dan proxy `/api` ke backend
| Backend API	| Go 1.25 + Gin	| Server REST API terkompilasi yang memory-safe dengan throughput tinggi
| ORM / Data Access	| Ent	| Schema-as-code type-safe entity framework untuk Go
| API Contract	| OpenAPI 3.0 + oapi-codegen |	Strictly typed kontrak API dengan pembuatan route handler Gin secara otomatis
| Kriptografi |	AES-256-GCM + Argon2id	| Enkripsi kredensial koneksi terautentikasi dan memory-hard password hashing
| Dukungan Database	| PostgreSQL 16+ & MySQL 8.0+	| Multi-engine managed database viewer dan migration executor
| Runtime & Monorepo |	Bun 1.4+	| Package manager, workspace runner, dan bundler JavaScript yang cepat
| Orkestrasi | Docker & Docker Compose | Containerized deployments dan pengujian integrasi terisolasi

---

## Prerequisites

- **[Bun](https://bun.sh/)** `>= 1.4.0`
- **[Go](https://go.dev/)** `>= 1.25.0` 
- **[Docker](https://www.docker.com/)** and **Docker Compose** — worker dijalankan sebagai service compose terpisah (`worker`) yang mengeksekusi job migration dari queue
- **Git** and **Bash** (Git Bash on Windows)
- Untuk demo dataset besar: `psql` client. Seed script: `scripts/native/task-5.sh` (mengisi ~2–5 juta baris dan mengukur durasi UPDATE tanpa batching; jumlah row + durasi dicatat untuk laporan M2)

---

## Quickstart with Docker Compose

Cara tercepat untuk menjalankan Orpbal beserta semua Servicenya (Web, Backend, Database Platform PostgreSQL, dan sample Database Target) adalah menggunakan Docker Compose

### 1. Konfigurasi Environment Backend
Salin berkas konfigurasi backend dari templat:

```sh
cp apps/server/.env.example apps/server/.env
```

Buat kunci enkripsi 32-byte acak dalam format base64 yang aman:

```sh
# Di Linux / macOS / Git Bash:
openssl rand -base64 32
```

Buka `apps/server/.env` dan masukkan kunci yang telah dibuat:

```env
ORPBAL_ENCRYPTION_KEY=<kunci-base64-hasil-generate>
ORPBAL_BOOTSTRAP_ADMIN_USERNAME=admin
ORPBAL_BOOTSTRAP_ADMIN_PASSWORD=Admin!Secret2026
```

### 2. Jalankan Seluruh Service

```sh
docker compose up --build
```

### 3. Akses Service

Setelah semua health check berhasil dilewati, service dapat diakses melalui:

| Service | URL / Port | Kredensial / Catatan |
|---|---|---|
| **Frontend Web** | `http://localhost:3000` | Masuk menggunakan akun admin bootstrap |
| **Backend API** | `http://localhost:8080` | Health Endpoint : `http://localhost:8080/health` |
| **Platform PostgreSQL** | `localhost:5432` | User: `postgres`, Password: `postgres`, DB: `Orpbal` |
| **Sample Target PostgreSQL**| `localhost:5433` | User: `postgres`, Password: `postgres`, DB: `userdb` |

Buka `http://localhost:3000/login` dan masuk dengan kredensial admin bootstrap Anda. Dari menu navigasi admin, Anda dapat langsung mendaftarkan `localhost:5433` (atau jaringan internal `db-user:5432`) sebagai koneksi database terkelola.

---

## Pengembangan Lokal Manual

Untuk proses pengembangan langsung pada mesin lokal:

### 1. Pasang Dependensi

```sh
# Pasang dependensi frontend di workspace
bun install

# Unduh modul Go untuk backend
(cd apps/server && go mod tidy)
```

### 2. Konfigurasi Environment

**Backend:**
```sh
cp apps/server/.env.example apps/server/.env
```
Perbarui `apps/server/.env` dengan string koneksi PostgreSQL platform Anda dan nilai `ORPBAL_ENCRYPTION_KEY` baru:
```env
APP_ENV=development
APP_ORIGIN=http://localhost:4321
DATABASE_URL=postgres://postgres:postgres@localhost:5432/Orpbal?sslmode=disable
ORPBAL_ENCRYPTION_KEY=<base64-encoded-32-bytes>
ORPBAL_BOOTSTRAP_ADMIN_USERNAME=admin
ORPBAL_BOOTSTRAP_ADMIN_PASSWORD=AdminPassword!123
```

**Frontend:**
```sh
cp apps/web/.env.example apps/web/.env
```
Pastikan `SERVER_URL` mengarah ke instance backend:
```env
SERVER_URL=http://localhost:8080
```

### 3. Jalankan Database Platform

Jalankan instance PostgreSQL 16 lokal. Anda dapat menggunakan Docker untuk menjalankannya:

```sh
docker run --name orpbal-postgres -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=Orpbal -p 5432:5432 -d postgres:16-alpine
```

### 4. Jalankan  Development Server

Jalankan frontend dan backend secara bersamaan dari root workspace:

```sh
bun run dev
```

Atau jalankan masing-masing pada terminal terpisah:

```sh
# Terminal 1: Backend API
cd apps/server && go run cmd/server/main.go

# Terminal 2: Vite SPA dev server (dengan proxy /api → :8080)
bun run --filter web dev
```

Buka `http://localhost:4321` di browser Anda. Server pengembangan Vite mem-
proxy seluruh trafik `/api/*` ke Go Gin API, sehingga browser tetap
same-origin (cookie sesi otomatis terlampir tanpa konfigurasi CORS).

---

## Konfigurasi Environment Variables

### Backend (`apps/server/.env`)

| Variabel | Wajib | Default | Deskripsi |
|---|---|---|---|
| `DATABASE_URL` | **Ya** | - | String koneksi database PostgreSQL internal platform |
| `ORPBAL_ENCRYPTION_KEY` | **Ya** | - | Kunci 32-byte berformat base64 (`openssl rand -base64 32`) untuk mengenkripsi kredensial target. **Jangan ubah kunci ini tanpa mengenkripsi ulang data yang sudah ada.** |
| `ORPBAL_BOOTSTRAP_ADMIN_USERNAME` | Tidak | `admin` | Username untuk administrator platform pertama |
| `ORPBAL_BOOTSTRAP_ADMIN_PASSWORD` | Tidak | `Admin!23` | Kata sandi untuk administrator bootstrap (minimal 8 karakter) |
| `APP_ENV` | Tidak | `development` | Atur ke `production` di server rilis (mengaktifkan cookie `Secure`) |
| `APP_ORIGIN` | Tidak | `http://localhost:4321` | Origin web frontend yang diizinkan |
| `HOST` | Tidak | `0.0.0.0` | Host bind untuk server HTTP Gin |
| `PORT` | Tidak | `8080` | Port bind untuk server HTTP Gin |
| `LOG_FORMAT` | Tidak | *(kosong = JSON)* | Format log terstruktur; kosong menghasilkan JSON di semua mode, isi `console` untuk format ramah-manusia saat pengembangan |
| `ORPBAL_SESSION_IDLE_TIMEOUT` | Tidak | `30m` | Durasi inaktivitas sebelum sesi kedaluwarsa |
| `ORPBAL_SESSION_ABSOLUTE_TIMEOUT` | Tidak | `8h` | Batas maksimum masa pakai sesi terlepas dari adanya aktivitas |
| `ORPBAL_SESSION_ROTATION_INTERVAL` | Tidak | `15m` | Interval waktu rotasi token sesi otomatis |
| `ORPBAL_SESSION_GRACE_PERIOD` | Tidak | `30s` | Periode toleransi token lama tetap valid untuk mencegah *race condition* |

### Worker (`apps/server/cmd/worker`)

| Variabel | Wajib | Default | Deskripsi |
|---|---|---|---|
| `DATABASE_URL` | **Ya** | - | Platform PostgreSQL yang menyimpan tabel job queue |
| `ORPBAL_ENCRYPTION_KEY` | **Ya** | - | Kunci yang sama dengan server (dekripsi kredensial target) |
| `WORKER_ID` | Tidak | hostname | Identitas worker untuk claim/heartbeat baris job |
| `ORPBAL_POLL_INTERVAL` | Tidak | `1s` | Jeda polling queue ketika tidak ada job |

### Frontend (`apps/web/.env`)

Hanya relevan untuk **server pengembangan** (dev). Pada produksi, nginx
mem-proxy `/api` ke backend secara langsung, jadi nilai ini tidak digunakan.

| Variabel | Wajib | Default | Deskripsi |
|---|---|---|---|
| `SERVER_URL` | Tidak | `http://localhost:8080` | Alamat Go Gin API yang dituju proxy `/api/*` oleh Vite saat dev |
| `HOST` | Tidak | `0.0.0.0` | Alamat bind server pengembangan Vite |
| `PORT` | Tidak | `4321` (dev) / `3000` (prod) | Port server frontend |

---

### Kebijakan Keamanan Sesi
Dikonfigurasi pada `apps/server/internal/security/session/policy.go`:

| Parameter Kebijakan | Nilai | Perilaku |
|---|---|---|
| **Idle Timeout** | `30 menit` | Sesi ditutup jika tidak ada aktivitas request HTTP selama 30 menit. |
| **Absolute Expiration** | `8 jam` | Pengguna wajib login kembali setelah 8 jam meskipun terus aktif. |
| **Rotation Interval** | `15 menit` | Token sesi dirotasi secara otomatis setiap 15 menit untuk membatasi risiko pembajakan token (replay attacks). |
| **Grace Period** | `30 detik` | Selama rotasi token, token lama tetap dapat diterima selama 30 detik guna mencegah kegagalan request paralel. |

### Keamanan Data
- Kata sandi database target dienkripsi menggunakan AES-256-GCM dengan kunci `ORPBAL_ENCRYPTION_KEY`. Database platform hanya menyimpan teks terenkripsi (*ciphertext*) dan tag autentikasi.
- Kata sandi pengguna di-hash menggunakan algoritma `Argon2id` dengan salt acak yang aman.
- Setiap sesi terautentikasi memiliki token CSRF terenkripsi. Seluruh metode API yang mengubah status data (`POST`, `PATCH`, `DELETE`) wajib menyertakan token ini pada header `X-CSRF-Token`.
- Query Sandboxing:
  - Koneksi PostgreSQL memberlakukan transaksi `SET TRANSACTION READ ONLY`.
  - Koneksi MySQL menonaktifkan fitur multi-statement (`MultiStatements=false`) serta divalidasi dengan parser AST untuk memblokir sintaks DDL atau DML di konsol query.
- **Redaksi log** (`internal/sqllog`): sebelum SQL, pesan error, atau DSN mencapai log maupun response, kata sandi DSN (`postgres://user:***@host`), literal sensitif (`password = '***'`, `token: '***'`, `PASSWORD('***')`), dan kredensial lain disamarkan; SQL dipotong pada 200 karakter. Unit test `internal/sqllog` mengunci perilaku ini.

## Activity Logging & Tracing (JSON terstruktur)

Kedua proses (API dan worker) memancarkan **log JSON terstruktur** ke stdout melalui zerolog di semua lingkungan (format console ramah-manusia opsional via `LOG_FORMAT=console`); log **tidak pernah disimpan** di database platform. Setiap eksekusi SQL — termasuk query read-only konsol, baris tabel, EXPLAIN, introspeksi katalog, mutasi baris, dan migration batch — dicatat pada lapisan `internal/target` dengan atribut wajib spesifikasi:

| Atribut | Sumber |
| :--- | :--- |
| `event`, `status` | `sql_query_start/done`, `sql_exec_start`, `sql_commit`, `sql_rollback`, `sql_explain_done`, `batch_commit`, `exec_error` |
| `actor_id`, `actor`, `is_platform_admin` | Middleware API (setelah autentikasi) |
| `database`, `database_id`, `engine` | Identity target (`Manager.Open`) |
| `trace_id`, `span_id` | Span OpenTelemetry per request (`tracing.GinMiddleware`) |
| `sql` (redacted + truncated), `param_count` | Setiap statement |
| `rows`, `duration_ms`, `error`, `sqlstate` | Hasil eksekusi; commit dan rollback dicatat sebagai event terpisah |
| `migration_id`, `execution_id`, `attempt`, `job_id`, `batch` | Worker (korelasi penuh rantai API → queue → worker → lock → batch) |

Contoh baris log (mulai dan hasil dicatat sebagai event terpisah; commit dan rollback juga terpisah):

```json
{"level":"info","time":"2026-11-27T09:15:04.101Z","actor_id":3,"actor":"devi","trace_id":"4bf92f3577b34da6a3ce929d0e0e4736","span_id":"00f067aa0ba902b7","component":"api","database":"sales_prod","database_id":2,"engine":"postgres","event":"sql_query_start","status":"running","sql":"SELECT * FROM orders WHERE id > 1000 ORDER BY id LIMIT 100","message":"read-only query started"}
{"level":"info","time":"2026-11-27T09:15:04.183Z","actor_id":3,"actor":"devi","trace_id":"4bf92f3577b34da6a3ce929d0e0e4736","span_id":"00f067aa0ba902b7","database":"sales_prod","event":"sql_query_done","status":"success","sql":"SELECT * FROM orders WHERE id > 1000 ORDER BY id LIMIT 100","rows":100,"row_limit":100,"duration_ms":82,"phase":"commit","message":"read-only query finished"}
{"level":"info","time":"2026-11-27T09:15:20.640Z","trace_id":"4bf92f3577b34da6a3ce929d0e0e4736","span_id":"23a1b0c9d4e5f617","job_id":41,"execution_id":88,"attempt":1,"connection_id":2,"migration_id":12,"event":"batch_commit","batch":7,"rows":10000,"duration_ms":412,"message":"batch committed"}
{"level":"error","time":"2026-11-27T09:16:02.512Z","trace_id":"4bf92f3577b34da6a3ce929d0e0e4736","database":"sales_prod","event":"sql_rollback","status":"rolled_back","sql":"UPDATE notes SET archived = true WHERE ...","error":"pq: deadlock detected","sqlstate":"40P01","duration_ms":1014,"message":"statement failed; transaction rolled back"}
```

**Tracing OpenTelemetry**: server API membuka span `HTTP <method> <route>` per request (menyambung `traceparent` masuk via `tracing.GinMiddleware`), `traceparent` W3C disimpan pada baris job saat approval, dan worker melanjutkan trace yang sama pada span `worker.execute` → lock → batch/query — rantai span API → queue → worker tersambung penuh. Contoh span (exporter console/JSON):

```json
{
  "name": "worker.execute",
  "traceId": "4bf92f3577b34da6a3ce929d0e0e4736",
  "spanId": "23a1b0c9d4e5f617",
  "parentSpanId": "00f067aa0ba902b7",
  "attributes": {"http.route": "/databases/2/migrations", "http.status_code": 200}
}
```

---

## Observability: Metrik Prometheus + Dashboard Grafana

Selain log dan trace, platform mengekspos metrik Prometheus siap-scrape:

| Sumber | Endpoint | Metrik utama |
| :--- | :--- | :--- |
| API server | `GET /api` host → `:8080/metrics` (tanpa auth, jangan dipublikasikan ke publik) | `orpbal_http_requests_total{route,method,status}`, `orpbal_http_request_duration_seconds_bucket{route,method}`, gauge `orpbal_migration_jobs{status}` (refresh 15 s dari `migration_jobs`), kolektor proses/Go standar |
| Worker | `WORKER_METRICS_ADDR` (default `:9102/metrics`) | `orpbal_migration_jobs_claimed_total{worker}`, `orpbal_migration_job_outcomes_total{status}` (success/failed/retry/cancelled/partial), `orpbal_migration_batches_committed_total`, `orpbal_migration_batch_rows_total`, `orpbal_migration_attempts_total{kind}` |

Stack observability dihidupkan bersama compose:

```sh
docker compose up -d            # seluruh stack termasuk prometheus + grafana
# Prometheus UI : http://localhost:9090  (scrape api + worker tiap 15s, alert rules aktif)
# Grafana UI    : http://localhost:3001  (login admin / ${GRAFANA_ADMIN_PASSWORD:-admin})
#   - datasource Prometheus otomatis (provisioning), dashboard "Orpbal Overview" otomatis termuat
```

Komponen:

| Berkas | Isi |
| :--- | :--- |
| `deploy/prometheus/prometheus.yml` | Scrape jobs `orpbal-api` (`app:8080`) dan `orpbal-worker` (`worker:9102`), retensi 7 hari |
| `deploy/prometheus/alerts.yml` | `OrpbalMigrationJobStuck` (running > 10 m), `OrpbalMigrationQueueBacklog` (queued > 15 m), `OrpbalMigrationFailures` (> 3 kegagalan terminal/15 m), `OrpbalHighHTTPErrorRate` (5xx > 5% selama 5 m) |
| `deploy/grafana/provisioning/` | Provisioning otomatis datasource + dashboard provider |
| `deploy/grafana/dashboards/orpbal-overview.json` | Dashboard: request rate per route, latency p95, tingkat 5xx, job per status, claims & outcomes, throughput batch (batches/s, rows/s), attempt per kind |

Instrumenasi hidup di `internal/metrics` (middleware gin untuk HTTP, counter/gauge dipanggil dari `internal/queue` dan `internal/worker`), dengan unit test di `internal/metrics/metrics_test.go`.

---

## Arsitektur Migration M2 (Asynchronous Workflow)

Milestone 2 mengubah eksekusi migration menjadi pipeline asinkron:

```mermaid
flowchart LR
  A[Draft] -->|submit| P[Pending Review]
  P -->|semua leader approve| Q[Queued]
  P -->|reject| A
  Q -->|worker: SKIP LOCKED + advisory lock| R[Running]
  R --> S[Success]
  R --> F[Failed / retry]
  R --> Part[Partial - batch]
  Part -->|resume| R
  S -->|rollback| RB[Rollback eksekusi]
  RB -->|forward| FW[Forward eksekusi]
```

Komponen:

| Komponen | Lokasi | Peran |
| :--- | :--- | :--- |
| Queue | `apps/server/internal/queue` | Job queue di PostgreSQL (`FOR UPDATE SKIP LOCKED`), heartbeat, checkpoint, retry transient (maks 3 attempt), stale-claim recovery, advisory lock per connection (`pg_try_advisory_lock`), propagation `traceparent` API→worker |
| Worker | `apps/server/cmd/worker` + `internal/worker` | Proses terpisah (service compose `worker`) yang meng-claim job, menjalankan SQL up/down, snapshot schema, dan mode batch dengan cursor |
| Workflow service | `apps/server/internal/service/migration.go` | Draft → submit → pending_review → approval (semua leader group yang punya grant view/view_edit) → queued; rollback hanya pada migration aktif terakhir yang reversible; forward/retry; batch revert |
| Handler SPA | `apps/server/internal/handler/migration2_handler.go` | Endpoint API baru (submit/rollback/forward/revert-batches/approvals/jobs) |
| SPA | `apps/web` | Halaman Migrations (authoring + history + filter Hide rollback), JobDetail (polling 2s), Approvals (inbox leader) |

Keputusan desain penting:

- **Checkpoint per batch** ditulis ke database platform **setelah** commit batch di target (cross-DB 2PC ditolak); crash di antara keduanya berarti satu batch idempoten dijalankan ulang — template batch wajib idempoten.
- **Rollback memakai SQL down asli** dari revisi migration yang dieksekusi (immutable); SQL yang pernah dieksekusi disalin ke `sql_executed` dan tidak pernah diubah.
- **Riwayat attempt bersifat append-only**: tabel `migration_attempts` (entitas baru) menyimpan **satu baris imutabel per attempt** — setiap claim worker menulis baris baru dengan nomor attempt, salinan SQL yang benar-benar dijalankan, status akhir (success/failed/partial/cancelled), error+SQLSTATE, pekerja, dan waktu. Attempt gagal maupun berhasil tidak pernah ditimpa atau dihapus; `migration_executions` hanya mencerminkan status attempt terkini untuk tampilan daftar, dan retry transient tidak lagi mengembalikan baris execution ke `queued` (kini `failed` sampai retry benar-benar berjalan). Riwayat attempt tampil di SPA (MigrationsPage per eksekusi) dan API (`execution.attempts`). Baris approval di-reset per siklus review (keputusan siklus sebelumnya tergantikan) — dipilih demi unique index `(migration_id, group_id)` yang sudah ada di database produksi.
- **Gate urutan versi (linear history)**: approval terakhir sebuah migration **up** ditolak `409 version_order` bila masih ada versi lebih kecil pada database yang sama yang belum selesai (draft/pending_review/queued/running/partial). Rollback/forward/retry/batch_revert dikecualikan karena mengikuti urutan yang sudah ada; versi yang gagal terminal tidak memblokir karena kegagalannya sudah menjadi entri permanen riwayat.
- **Gate partial antar-migration**: claim worker menolak job up/forward baru pada sebuah koneksi selama masih ada job berstatus `partial` di koneksi yang sama (beberapa batch sudah committed); hanya `retry` dan `batch_revert` — jalur keluar yang disetujui dari status partial — yang tetap bisa di-claim.
- **Batch revert memakai kompensasi per batch** dalam urutan maju atas rentang cursor yang sama (template disetujui leader saat revert, fallback SQL down asli) — deviasi sadar dari redaksi spesifikasi yang meminta SQL down immutable urutan batch terbalik; alurnya tetap melewati permission, validasi, dan approval leader penuh.
- **Lock recovery**: worker mati terdeteksi lewat `heartbeat_at` kedaluwarsa (`lockTTL`); job di-claim ulang worker lain, checkpoint/eksekusi diverifikasi sebelum retry; attempt yang ditinggalkan ditutup `failed` (tidak menggantung `running`).
- **Hapus koneksi database** ditolak `409` bila masih ada migration/riwayat di targetnya (admin men-disable lewat `is_enabled`); koneksi tanpa riwayat dihapus bersama grant-nya. `POST /api/admin/databases/{id}/test` menguji koneksi tersimpan (termasuk yang disabled) tanpa mengubah apa pun.

### Dataset besar untuk pengujian batch

`scripts/native/task-5.sh` mengisi tabel `orpbal_demo_events` (~2 juta baris default, `SEED_ROWS` untuk override) dan mengukur durasi UPDATE tanpa batching (dalam transaksi yang di-ROLLBACK, jadi tidak mengubah data):

```
Rows seeded. Unbatched UPDATE timing (spec requires > 1s; probe runs in a rolled-back transaction):
UPDATE ... WHERE tenant_id = 1 → Time: 2400.123 ms
```

Angka terukur terbaru (PostgreSQL 17 lokal, `TestWorkerLargeDatasetBatch`, dataset sama untuk kedua mode):

| Mode | Durasi | Rows |
| :--- | :--- | :--- |
| UPDATE tanpa batching | 1,21 s | 500.000 |
| Migration per batch (10k/batch, 50 batch) | 3,64 s | 500.000 |

Test mengassert `rows_done == jumlah baris` dan semua row berstatus target, sehingga regresi batching gagal test. `TestWorkerLargeDatasetBatch` (`bigdataset_test.go`) mengkalibrasi jumlah baris otomatis (doubling dari 250k, cap 8M) sampai satu UPDATE tanpa batching melewati 1 detik, lalu memigrasi dataset yang **sama** per batch — angka di atas adalah hasil run 27 November 2026.

---

## Role-Based Access Control (RBAC)

Orpbal menerapkan pembagian kontrol akses berjenjang:

1. Administrator Platform:
   - Mengelola akun pengguna platform dan mereset kredensial.
   - Membuat, memperbarui, dan menghapus Grup Pengguna.
   - Menambah, mengubah, menguji, dan mengaktifkan/menonaktifkan koneksi database terkelola.
   - Menetapkan dan mencabut hak akses database (*Database Grants*) ke Grup.

2. Database Grants & Hak Akses Efektif:
   - Hak akses database hanya diberikan melalui perantara **Grup**.
   - Jika pengguna tergabung dalam beberapa grup yang memiliki izin pada database yang sama, Orpbal akan otomatis menghitung tingkat izin efektif tertinggi:
     $$\text{Effective Access} = \max(\text{Grant}_1, \text{Grant}_2, \dots)$$

### Permissions Matrix

| Kemampuan | Platform Admin | Anggota Grup: `view_only` | Anggota Grup: `view_edit` |
|---|:---:|:---:|:---:|
| Mengelola User, Grup, & Koneksi Global | ✓ | X | X |
| Melihat Database & Diagram ER Skema | ✓ | ✓ | ✓ |
| Menjalankan Query SQL Read-Only di Konsol | ✓ | ✓ | ✓ |
| Melihat Baris Tabel & Statistik | ✓ | ✓ | ✓ |
| Menambah, Mengubah, atau Menghapus Baris Tabel | ✓ | X | ✓ |
| Merancang Migrasi Baru (DDL) | ✓ | X | ✓ |
| Mengeksekusi Migrasi yang Telah Dirancang | ✓ | X | ✓ |
| Melihat Riwayat Migrasi & Snapshot Diff | ✓ | ✓ | ✓ |

---

## Security: SAST (CodeQL)

Analisis statis keamanan (*Static Application Security Testing* / SAST) diaktifkan pada repository ini menggunakan **GitHub CodeQL**. Workflow-nya berada di [`.github/workflows/codeql.yml`](.github/workflows/codeql.yml) dan berjalan otomatis saat:

- `push` ke branch `main` (dan `master`),
- `pull_request` terhadap branch `main` (dan `master`),
- penjadwalan mingguan (`schedule` cron, setiap Minggu pukul 00:00 UTC).

### Cakupan Analisis

Analisis ditargetkan pada **backend Go** (`apps/server`, path sumber `apps/server`) menggunakan build mode `autobuild`. Paket query yang dijalankan:

- `security-extended` — seluruh query keamanan bawaan plus query tambahan yang tidak selalu diaktifkan secara default.
- `security-and-quality` — query keamanan ditambah query kualitas kode.

Hasil upload ke GitHub Code Scanning dan ditampilkan di tab *Security* repository.

### Proses Penanganan Temuan (*Findings*)

Setiap temuan CodeQL didokumentasikan dan diputuskan secara terstruktur. Untuk setiap finding, dicatat:

1. **Rule** — nama query / aturan CodeQL yang menghasilkan temuan.
2. **Severity** — level temuan (mis. `error` / `warning`) sebagaimana dilaporkan CodeQL.
3. **Location** — berkas, fungsi, dan baris kode tempat temuan terjadi.
4. **Impact** — dampak keamanan/risiko nyata terhadap sistem jika temuan dibiarkan.
5. **Keputusan: *fix* atau *accept*** — disertai justifikasi:
   - *Fix*: dilakukan perbaikan, dicatat commit-nya.
   - *Accept*: temuan diterima (mis. false positive atau risiko yang memang tidak berlaku pada konteks Orpbal), dicatat alasannya.
6. **Bukti rescan** — setelah commit perbaikan (jika ada), temuan diverifikasi ulang melalui scan CodeQL berikutnya dan dibuktikan bahwa temuan tidak lagi muncul pada rescan tersebut.

### Scan results

Hasil scan dipantau di [docs/codeql-findings.md](docs/codeql-findings.md): konfigurasi, riwayat run, dan tabel temuan lengkap (rule, severity, lokasi, dampak, keputusan fix/accept + justifikasi dan bukti rescan).

Ringkasan terkini (run `02aa9b0`, 2026-11-27 — scan sukses pertama):

- 5 alert `go/sql-injection` (high, CWE-089), semuanya di `internal/target` — platform ini sengaja mengeksekusi SQL yang disetujui/dibangun dari identifier tervalidasi, jadi temuan dievaluasi per lokasi.
- 2 lokasi diperbaiki: `PostgresDialect.GuardRead` kini menegakkan *keyword allowlist* read-only (commit `fac0f15`), bukan mengandalkan read-only transaction saja.
- 3 lokasi diterima dengan mitigasi berlapis (allowlist keyword, read-only transaction, identifier validation, immutable approved SQL) — rincian di dokumen temuan.

---

## Stress Testing (k6)

Pengujian beban menggunakan **k6** dengan skenario bertahap: naik ke beban normal → ditahan di atas beban normal (*peak*) → diturunkan kembali untuk mengamati pemulihan sistem. Script-nya ada di [`k6/stress.js`](k6/stress.js).

### Skenario

Dua skenario paralel:

| Skenario | Executor | Tahapan |
| :--- | :--- | :--- |
| `reads` | `ramping-vus` | 2m naik ke 10 VU (beban normal) → 1m tahan → 3m naik ke 50 VU (peak) → 2m tahan → 2m turun ke 0 |
| `jobs` | `ramping-arrival-rate` | 2m naik ke 1 job/10s → 3m naik ke 6 job/10s → 2m turun ke 0 |

Yang diuji: akses API baca (schema, migration history dengan filter `hide_rollback`) dan pengiriman job migration (rollback migration yang sudah approved) secara bersamaan. Respons pengiriman job (HTTP 202) **tidak** dihitung sebagai keberhasilan eksekusi migration — eksekusi dipantau lewat status job.

### Threshold

- `http_req_duration`: `p(95) < 500ms` (dan `p(50) < 200ms` sebagai pemantauan).
- `http_req_failed`: `rate < 5%`.

### Cara menjalankan

```bash
k6 run \
  -e BASE_URL=http://localhost:3000 \
  -e USERNAME=admin -e PASSWORD=... \
  -e CONNECTION_ID=1 \
  k6/stress.js
```

Prasyarat: service berjalan (docker compose up), minimal satu migration berstatus `success` yang reversible pada database target. Seed demo lengkap (2 migration approved + 2 juta baris dataset): `scripts/native/task-5.sh` setelah membuat koneksi `k6demo` via admin API. Contoh menjalankan k6 via docker tanpa instalasi lokal:

```bash
docker run --rm --network Orpbal_default \
  -e BASE_URL=http://web:3000 -e USERNAME=admin -e PASSWORD=... -e CONNECTION_ID=1 \
  -v "$PWD/k6/stress.js:/script.js" grafana/k6:latest run /script.js
```

### Hasil & Analisis

Hasil run penuh 10 menit (2026-11-27, commit `k6: alternate rollback/forward submissions`; lingkungan: docker compose di WSL2 8 core, Postgres 16 container, 2 juta baris dataset demo, 2 migration approved pada koneksi `k6demo`):

| Tingkat beban | p50 | p95 | Throughput req | Error rate (`http_req_failed`) | Keterangan |
| :--- | :--- | :--- | :--- | :--- | :--- |
| Ramp 0→10 VU (2m) | ~60–120 ms | ~200–300 ms | ~8–10/s | 0% | Semua threshold terpenuhi |
| Hold 10 VU (1m) | ~100 ms | ~250 ms | ~9/s | 0% | Stabil |
| Peak 50 VU + jobs 6/10s (3m) | ~900 ms | ~5 s | ~14/s | 0% | p95 melewati threshold 500 ms |
| Ramp down 50→0 (2m) | turun bertahap | pulih <300 ms | menurun | 0% | Pemulihan segera setelah peak berakhir |

Total run: **8.689 request, 2.896 iterasi, 0 failed request** (409 conflict dari race antar-VU pada migration yang sama diperhitungkan sebagai hasil yang diharapkan via `responseCallback`, bukan error). Checks: **100% sukses**.

**Job migration (queue → worker):** 6 job dieksekusi selama run; semua `success` (attempts=1, tanpa retry). Waktu eksekusi job terukur 2,9–8,5 s per job (termasuk snapshot schema sebelum/sesudah); durasi SQL statement 2,3–2,5 s per job karena migration bekerja pada ±28 ribu baris per tenant di dataset 2 juta baris. Tidak ada backlog queue — job yang masuk langsung di-claim worker (1 worker, polling 1s).

**Analisis p95 > 500 ms pada peak:**

1. **Sesi tunggal dibagi semua VU** — setiap request menyentuh (UPDATE `last_seen_at`, `idle_expires_at`) satu baris `sessions` yang sama, sehingga di 50 VU semua request mengantri pada lock baris tersebut. Probe terpisah menunjukkan latensi membaik drastis ketika request memakai beberapa sesi berbeda.
2. **Lingkungan berbagi CPU** — host WSL2 menjalankan beban lain (kubernetes, node, dsb.) pada 8 core yang sama; log menunjukkan `context canceled` (client timeout) hanya di menit-menit puncak ketika antrean paling panjang, dan CPU `app` mendominasi saat load (bukan `db`).
3. **Postgres kecil** (`shared_buffers=128MB`, container tanpa tuning) dan data uji kecil (±40 baris) — latency bukan dari query plan (semua query ter-index, eksekusi <2 ms di probe).

Perbaikan yang diidentifikasi: rotasi/sesi per-VU pada script (mengurangi lock baris), atau de-bounce `last_seen_at` di backend (mis. update maksimal 1× per menit per sesi). Keduanya tidak dilakukan di milestone ini karena bersifat tuning, bukan fungsionalitas — dicatat sebagai *known limit*.

Hasil angka mentah per run tersimpan pada `/tmp/k6out/summary*.json` saat pengujian; ringkasan di atas diambil dari output `k6 run` run terakhir (`run4`).

---

## Spesifikasi API & Pembuatan Kode

Backend menyediakan strictly-typed REST API  berbasis spesifikasi OpenAPI 3.0 yang terletak di [`apps/server/api/openapi.yaml`](apps/server/api/openapi.yaml).

### Ringkasan Rute Utama

- **Sistem & Autentikasi**:
  - `GET /api/health` - Status kesehatan Service
  - `POST /api/auth/login` - Autentikasi dan pembuatan sesi
  - `POST /api/auth/logout` - Pencabutan sesi
  - `GET /api/auth/session` - Informasi sesi aktif dan token CSRF
  - `POST /api/auth/password` - Mengubah kata sandi akun

- **Operasi & Eksplorasi Database**:
  - `GET /api/me/databases` - Daftar database yang dapat diakses pengguna
  - `GET /api/databases/{id}/schema` - Graf skema lengkap (tabel, kolom, PK/FK)
  - `POST /api/databases/{id}/query` - Eksekusi SQL read-only dalam lingkungan sandbox
  - `POST /api/databases/{id}/explain` - Rencana eksekusi EXPLAIN / EXPLAIN ANALYZE untuk query
  - `GET /api/databases/{id}/table-counts` - Statistik tabel & estimasi planner
  - `GET /api/databases/{id}/rows` - Data baris tabel dengan paginasi
  - `GET /api/databases/{id}/migrations` - Buku besar riwayat migrasi
  - `POST /api/databases/{id}/migrations` - Mengajukan migrasi baru
  - `GET /api/databases/{id}/migrations/{mId}/diff` - Perbandingan skema sebelum/sesudah
  - `POST /api/databases/{id}/migrations/{mId}/execute` - Menjalankan migrasi pada target

- **Administrasi Platform**:
  - `GET/POST /api/admin/users` - Melihat / membuat pengguna platform
  - `PATCH/DELETE /api/admin/users/{id}` - Memperbarui atau menghapus pengguna
  - `GET/POST /api/admin/groups` - Mengelola grup pengguna
  - `POST/DELETE /api/admin/groups/{id}/members/{userId}` - Keanggotaan grup
  - `GET/POST /api/admin/databases` - Mengelola koneksi database target
  - `PATCH/DELETE /api/admin/databases/{id}` - Memperbarui (verifikasi live) atau menghapus koneksi; delete ditolak bila riwayat migration ada
  - `POST /api/admin/databases/{id}/test` - Uji koneksi tersimpan dengan kredensial tersimpan (termasuk koneksi disabled)
  - `GET/POST /api/admin/grants` - Mengelola hak akses grup terhadap database

### Code Generation

Untuk membuat ulang kode Go dari skema Ent atau spesifikasi OpenAPI:

```sh
cd apps/server

# Membuat ulang entitas dan kueri Ent ORM
go generate ./ent

# Membuat ulang handler dan tipe server Gin dari openapi.yaml
go generate ./internal/api
```

---

## QA

### Verifikasi Frontend

```sh
# Pemeriksaan tipe untuk seluruh source TypeScript (SPA React + Vite)
bun run check-types

# Menjalankan unit test frontend menggunakan Vitest
bun run test

# Memvalidasi proses build produksi
bun run build
```

### Verifikasi Backend

```sh
cd apps/server

# Analisis statis dan pemeriksaan kode Go
go vet ./...

# Menjalankan unit test Go dan uji skema
go test ./...
```

### Uji Integrasi Menggunakan Docker

Untuk menguji API secara menyeluruh terhadap kontainer PostgreSQL dan MySQL sementara yang terisolasi:

```sh
# Menjalankan docker-compose.test.yml dan mengeksekusi rangkaian pengujian integrasi
bun run test:integration
```

Skrip memakai `docker compose` bila tersedia dan otomatis jatuh ke `docker-compose` pada instalasi lama. Tes integrasi mencakup alur sukses, kegagalan target, penolakan akses, siklus migration, dan smoke test MySQL 8.

---

## Referensi Perintah Workspace

`package.json` di root direktori menyediakan skrip untuk mengelola seluruh ekosistem monorepo:

| Skrip | Perintah | Fungsi |
|---|---|---|
| `bun run dev` | `concurrently ...` | Menjalankan web frontend dan backend Go secara bersamaan |
| `bun run build` | `bun run --filter '*' build` | Membangun semua package dan aplikasi web |
| `bun run check-types` | `bun run --filter '*' check-types` | Memeriksa tipe data web dan package TypeScript |
| `bun run test` | `bun run --filter web test` | Menjalankan seluruh pengujian web dengan Vitest |
| `bun run test:integration`| `bash scripts/test-integration.sh` | Menjalankan kontainer Postgres sementara & mengeksekusi test suite Go |
| `bun run dev:web` | `bun run --filter web dev` | Menjalankan server pengembangan Vite (SPA) |
| `bun run dev:server` | `bash scripts/native/task-1.sh` | Menjalankan server backend Go |
| `bun run setup:server` | `bash scripts/native/task-2.sh` | Merapikan dan memverifikasi modul Go |
| `bun run test:server` | `bash scripts/native/task-3.sh` | Merapikan modul Go dan menjalankan pengujian backend |

---

## Lisensi

Proyek ini dilisensikan di bawah lisensi **GNU Affero General Public License v3.0 (AGPL-3.0)**. Lihat berkas [LICENSE](LICENSE) untuk informasi lebih lanjut.