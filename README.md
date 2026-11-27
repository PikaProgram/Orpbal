
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
| **API metrics** | `http://localhost:8080/metrics` | Prometheus scrape endpoint (queue/DB pool) |
| **Worker metrics** | `http://localhost:9090/metrics` | Prometheus scrape endpoint (migration duration, batch/row, retries) |
| **Grafana** | `http://localhost:3001` | `admin` / `admin` (override with `GRAFANA_ADMIN_*`) |
| **Prometheus** | `http://localhost:9091` | Scrape config: `observability/prometheus.yml` |
| **OTel Collector** | `localhost:4317` (gRPC), `localhost:4318` (HTTP) | Menerima span & log dari API/worker |

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
| `LOG_LEVEL` | Tidak | `debug` | `production` menghasilkan JSON log (wajib untuk korelasi OTLP); mode lain memakai console writer |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Tidak | - | Endpoint Collector, mis. `http://otel-collector:4318`. Kosong = span/log tetap lokal (stdout + debug exporter) |
| `OTEL_SERVICE_NAME` | Tidak | `orpbal-server` | Nama service pada resource span/log |
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
| `ORPBAL_METRICS_ADDR` | Tidak | `:9090` | Alamat bind endpoint `/metrics` worker |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Tidak | - | Endpoint Collector untuk span/log worker |
| `OTEL_SERVICE_NAME` | Tidak | `orpbal-worker` | Nama service pada resource span/log worker |

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
- **Riwayat eksekusi tidak pernah dihapus**: attempt gagal tetap terlihat; filter *Hide rollback* murni view filter berbasis pasangan execution ID (`reverted_execution_id`).
- **Lock recovery**: worker mati terdeteksi lewat `heartbeat_at` kedaluwarsa (`lockTTL`); job di-claim ulang worker lain, checkpoint/eksekusi diverifikasi sebelum retry.

### Dataset besar untuk pengujian batch

`scripts/native/task-5.sh` mengisi tabel `orpbal_demo_events` (~2 juta baris default, `SEED_ROWS` untuk override) dan mengukur durasi UPDATE tanpa batching (dalam transaksi yang di-ROLLBACK, jadi tidak mengubah data):

```
Rows seeded. Unbatched UPDATE timing (spec requires > 1s; probe runs in a rolled-back transaction):
UPDATE ... WHERE tenant_id = 1 → Time: 2400.123 ms
```

Angka terukur di lingkungan demo (2.000.000 baris, batch 10.000):

| Mode | Durasi | Catatan |
| :--- | :--- | :--- |
| UPDATE tanpa batching | diisi setelah demo dijalankan | spesifikasi: > 1 detik |
| Migration per batch (10k/batch) | diisi setelah demo dijalankan | progres terlihat per batch di SPA |

### Verifikasi otomatis (CI)

Integrasi test `TestWorkerLargeDatasetBatch` (`apps/server/internal/worker/bigdataset_test.go`) memverifikasi ketentuan spesifikasi secara otomatis: kalibrasi jumlah baris di awal test (doubling dari 250k, cap 8M) sampai satu UPDATE tanpa batching melebihi 1 detik, lalu dataset yang **sama** dimigrasi per batch oleh worker (batch_size 10.000, checkpoint per batch). Hasil terukur di lingkungan CI lokal (tmpfs Postgres 16):

| Mode | Durasi | Rows |
| :--- | :--- | :--- |
| UPDATE tanpa batching | 6.85 s | 250.000 |
| Migration per batch (10k/batch, 25 batch) | 11.79 s | 250.000 |

Test mengassert `rows_done == jumlah baris` dan semua row berstatus target, sehingga regresi batching gagal test.

---

## Observability (Bonus B2)

Alur telemetry tambahan (instrumentasi logging/tracing wajib tetap dipakai; yang jadi bonus adalah collector terpusat, metrik Prometheus, dan dashboard Grafana):

```
API (span HTTP) ─┐
                 ├─ OTLP/HTTP → OTel Collector ─→ Loki (logs)
Worker (span) ───┘                              └─→ Tempo (traces)
       │
       └─ /metrics (worker) ─┐
API /metrics ────────────────┼─→ Prometheus → Grafana
Collector self-metrics ──────┘
```

Semua konfigurasi ada di `observability/`:

| File | Fungsi |
| :--- | :--- |
| `otel-collector-config.yaml` | Receiver OTLP (gRPC 4317 / HTTP 4318), processor redaksi field sensitif, exporter Loki + Tempo + debug |
| `loki-config.yaml` | Backend log lokal (filesystem) |
| `tempo-config.yaml` | Backend trace lokal + receiver OTLP |
| `prometheus.yml` | Scrape `app:8080`, `worker:9090`, dan `otel-collector:8889` |
| `grafana/provisioning/` | Datasource Prometheus/Loki/Tempo + *derived field* `trace_id` → Tempo |
| `grafana/dashboards/orpbal-observability.json` | Dashboard: job per status, queue/lock wait, durasi migration, koneksi DB, retry/batch, log, trace |

Jalankan seperti biasa; kelima service observability ikut `docker compose up --build`:

```sh
docker compose up --build
# Grafana: http://localhost:3001 (admin/admin)
# Prometheus: http://localhost:9091
```

### Metrik Prometheus

| Metrik | Label (kardinalitas rendah) | Sumber |
| :--- | :--- | :--- |
| `orpbal_jobs` | `status` | Query `migration_jobs` saat scrape (API) |
| `orpbal_job_claims_total` | `result` (`claimed`, `contended`, `empty`, `error`) | `queue.Claim` |
| `orpbal_job_queue_wait_seconds` | - | `created_at` → claim |
| `orpbal_job_lock_wait_seconds` | - | Connection checkout + `pg_try_advisory_lock` |
| `orpbal_job_retries_total` | - | `queue.FinishFailed` saat transient retry |
| `orpbal_job_finishes_total` | `status` | FinishSuccess/FinishFailed/stopBatch |
| `orpbal_migration_duration_seconds` | `kind` (`up`, `rollback`, `forward`, `retry`, `batch_revert`) | Worker `run` |
| `orpbal_batches_total`, `orpbal_rows_total`, `orpbal_batch_duration_seconds` | `kind` | Tiap batch committed |
| `orpbal_db_pool_connections` | `pool`, `state` (`open`, `idle`, `in_use`, `max_open`) | `database/sql.DB.Stats()` |
| `orpbal_db_pool_wait_seconds_total`, `orpbal_db_pool_wait_count_total` | `pool` | `database/sql.DB.Stats()` |

Label tidak pernah memakai user ID, job ID, SQL, DSN, atau session identifier. Selain metrik di atas, Go runtime/process metrics tersedia otomatis dari `promhttp.Handler()`.

### Contoh korelasi log → trace → metrik

Satu submission migration menghasilkan `traceparent` di baris job; worker melanjutkan trace yang sama dan menuliskan `trace_id`/`span_id` ke log (ID di bawah hanya ilustrasi):

```json
{"level":"info","trace_id":"4bf92f3577b34da6a3ce929d0e0e4736","span_id":"00f067aa0ba902b7","event":"batch_commit","batch":7,"rows":10000,"duration_ms":812,"database":"userdb","status":"running","time":"2026-11-27T05:12:33Z","message":"batch committed"}
```

Span yang diterima Collector (debug exporter) memiliki trace ID yang sama, sehingga tombol *trace_id* di panel log Loki membuka trace di Tempo:

```json
{"Name":"worker.execute","SpanContext":{"TraceID":"4bf92f3577b34da6a3ce929d0e0e4736","SpanID":"00f067aa0ba902b7","TraceFlags":"01"},"StartTime":"2026-11-27T05:12:20Z","EndTime":"2026-11-27T05:12:34Z","Duration":14.2}
```

Pada rentang waktu yang sama, Grafana/Prometheus menampilkan metrik dari eksekusi tersebut:

```
orpbal_job_queue_wait_seconds_bucket{le="1",instance="worker:9090"} 12
orpbal_migration_duration_seconds_bucket{kind="up",le="30",instance="worker:9090"} 3
orpbal_db_pool_connections{pool="platform",state="in_use",instance="app:8080"} 2
```

Credential, DSN, cookie, token, serta session identifier dihapus oleh `attributes/redact` di Collector dan oleh regex `scrubText` di `internal/observability/log.go`; teks SQL tidak dipakai sebagai label metrik maupun atribut span (hanya verb operasinya, mis. `db.operation="UPDATE"`).

> Mode OTLP mengharuskan `LOG_LEVEL=production` (JSON log) agar field `trace_id`/`span_id` dapat diekstrak. Tanpa `OTEL_EXPORTER_OTLP_ENDPOINT`, perilaku lama tetap berjalan: span JSON ke stdout dan log console/JSON sesuai `LOG_LEVEL`.

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

Smoke test endpoint metrik (saat compose berjalan):

```sh
curl -s http://localhost:8080/metrics | grep orpbal_
curl -s http://localhost:9090/metrics | grep orpbal_
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