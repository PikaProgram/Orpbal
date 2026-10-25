
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
│       └── ci.yml               
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
│       │   ├── actions/ 
│       │   ├── components/
│       │   ├── layouts/  
│       │   ├── lib/
│       │   ├── pages/ 
│       │   │   ├── admin/
│       │   │   ├── api/
│       │   │   ├── databases/ 
│       │   │   ├── login.astro  
│       │   │   └── logout.astro
│       │   └── styles/  
│       ├── Dockerfile.vite
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
| Frontend Framework |	Astro 7 |	Aplikasi web Server-Side Rendered (SSR) berkinerja tinggi dengan adapter Node
| Interactive UI |	React 19 |	Komponen interaktif reaktif sisi klien untuk kanvas dan editor
| Diagram Engine |	@xyflow/react + Dagre |	Editor graf visual interaktif dengan tata letak otomatis untuk diagram relasional
| SQL Editor |	CodeMirror 6	| Editor web canggih dengan penyorotan sintaksis SQL, nomor baris, dan pelengkapan otomatis
| Styling |	Tailwind CSS v4	| Styling modern menggunakan CSS
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
- **[Docker](https://www.docker.com/)** and **Docker Compose** 
- **Git** and **Bash** (Git Bash on Windows)

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

# Terminal 2: Aplikasi Web Astro SSR
bun run --filter web dev
```

Buka `http://localhost:4321` di browser Anda.

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
| `LOG_LEVEL` | Tidak | `debug` | Level log terstruktur (`debug`, `info`, `warn`, `error`, `production`) |
| `ORPBAL_SESSION_IDLE_TIMEOUT` | Tidak | `30m` | Durasi inaktivitas sebelum sesi kedaluwarsa |
| `ORPBAL_SESSION_ABSOLUTE_TIMEOUT` | Tidak | `8h` | Batas maksimum masa pakai sesi terlepas dari adanya aktivitas |
| `ORPBAL_SESSION_ROTATION_INTERVAL` | Tidak | `15m` | Interval waktu rotasi token sesi otomatis |
| `ORPBAL_SESSION_GRACE_PERIOD` | Tidak | `30s` | Periode toleransi token lama tetap valid untuk mencegah *race condition* |

### Frontend (`apps/web/.env`)

| Variabel | Wajib | Default | Deskripsi |
|---|---|---|---|
| `SERVER_URL` | Tidak | `http://localhost:8080` | Alamat Go Gin API. Diproxy ke `/api/*` oleh layer Astro SSR |
| `HOST` | Tidak | `0.0.0.0` | Alamat bind server frontend |
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
# Pemeriksaan tipe untuk semua file Astro dan TypeScript
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
| `bun run dev:web` | `bun run --filter web dev` | Menjalankan server pengembangan Astro SSR |
| `bun run dev:server` | `bash scripts/native/task-1.sh` | Menjalankan server backend Go |
| `bun run setup:server` | `bash scripts/native/task-2.sh` | Merapikan dan memverifikasi modul Go |
| `bun run test:server` | `bash scripts/native/task-3.sh` | Merapikan modul Go dan menjalankan pengujian backend |

---

## Lisensi

Proyek ini dilisensikan di bawah lisensi **GNU Affero General Public License v3.0 (AGPL-3.0)**. Lihat berkas [LICENSE](LICENSE) untuk informasi lebih lanjut.