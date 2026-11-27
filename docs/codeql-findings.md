# CodeQL SAST — Temuan dan Keputusan

Static Application Security Testing (SAST) dengan [CodeQL](https://codeql.github.com/docs/codeql-api-overview/) diaktifkan pada repository ini melalui GitHub Actions.

## Konfigurasi

| Aspek | Nilai |
| :--- | :--- |
| Workflow | [`.github/workflows/codeql.yml`](../.github/workflows/codeql.yml) |
| Bahasa | Go |
| Cakupan kode | `apps/server/**` (backend Go: API server dan worker) |
| Query suite | `security-extended`, `security-and-quality` |
| Build mode | `autobuild` |
| Pemicu | push ke `main`, pull request, jadwal mingguan (Senin 00:00 UTC), manual (`workflow_dispatch`) |
| Hasil | GitHub Security tab → Code scanning alerts (SARIF) |

## Riwayat scan

| Tanggal | Commit | Hasil | Catatan |
| :--- | :--- | :--- | :--- |
| 2026-11-27 | `02aa9b0` | ✅ sukses | 5 alert `go/sql-injection` (semua di `internal/target`) |
| 2026-11-27 | `fac0f15` | ✅ sukses | perbaikan diterapkan, alert dievaluasi (lihat bawah) |

Scan pertama sebelum 2026-11-27 selalu gagal (*configuration error*): paket `internal/schema/build` tidak ter-commit karena aturan `.gitignore` root (`build` tanpa anchor) menelan direktori tersebut, sehingga build Go di runner gagal dan tidak ada database analisis. Diperbaiki di `29877e4` + `02aa9b0`.

## Temuan

Semua 5 alert adalah rule yang sama — `go/sql-injection` (*Database query built from user-controlled sources*), severity **high**, CWE-089:

| # | Rule | Severity | Lokasi | Dampak potensial | Keputusan |
| :--- | :--- | :--- | :--- | :--- | :--- |
| 1 | `go/sql-injection` | high | `internal/target/target.go:67` (`Target.Query` → `QueryContext(ctx, statement)`) | SQL dari pemanggil dieksekusi apa adanya | **Diperbaiki** (lihat bawah) |
| 2 | `go/sql-injection` | high | `internal/target/target.go:119` (`Target.ExecuteArgs` → `ExecContext(ctx, statement, args...)`) | sama | **Diterima** (lihat bawah) |
| 3 | `go/sql-injection` | high | `internal/target/postgres.go:184` (`PostgresDialect.Explain` → `QueryContext(ctx, query)`, query = `"EXPLAIN (...)" + statement`) | sama | **Diterima** |
| 4 | `go/sql-injection` | high | `internal/target/target.go:65` (`Target.Query`, jalur `t.db.QueryContext` non-tx) | sama | **Diperbaiki** |
| 5 | `go/sql-injection` | high | `internal/target/mysql.go:185` (`MySQLDialect.Explain` → `QueryContext(ctx, prefix+statement)`) | sama | **Diterima** |

Link alert: [Security → Code scanning](https://github.com/PikaProgram/Orpbal/security/code-scanning).

## Keputusan perbaikan

### Diperbaiki (alert 1 & 4)

`PostgresDialect.GuardRead` hanya memeriksa *non-empty input* — penulisan (INSERT/UPDATE/DELETE) ke SQL viewer PostgreSQL diblok murni oleh *read-only transaction* di level database. Ditambahkan *keyword allowlist* yang sama dengan dialek MySQL (`SELECT`, `SHOW`, `EXPLAIN`, `DESCRIBE`, `DESC`, `WITH`), sehingga endpoint viewer/explain menolak statement non-read dengan 400 deterministik sebelum menyentuh database.

- Perbaikan: commit `fac0f15` (`apps/server/internal/target/postgres.go`)
- Verifikasi: probe test menunjukkan `EXPLAIN ANALYZE INSERT` dan `Query(INSERT)` ditolak oleh PostgreSQL (`cannot execute INSERT in a read-only transaction`) **sebelum** perubahan, dan kini ditolak lebih awal oleh *guard*; suite `internal/target` dan `internal/handler` lulus.
- Scan ulang: alert dievaluasi ulang pada run berikutnya; alur data tidak berubah secara struktural (statement dari pengguna tetap sengaja dijalankan — platform ini adalah *SQL console*), tetapi *guard* kini menegakkan semantik *read-only* di aplikasi, bukan hanya di driver.

### Diterima (alert 2, 3, 5)

- **Alert 2** (`ExecuteArgs`): parameter tanda tangan sengaja menerima SQL mentah — dipanggil oleh (a) migrasi pekerja dengan SQL yang **immutable & disetujui** (disalin ke `migration_executions.sql_executed`, lolos alur persetujuan), (b) mutasi baris yang dibangun oleh `Dialect.Mutation` dengan **placeholder** dan **identifier yang dikutip/divalidasi** (`QuoteIdent` + regex `^[a-zA-Z_][a-zA-Z0-9_]*$`), bukan interpolasi pengguna. *Risk* diterima: API ini adalah primitif eksekusi internal, tidak pernah menerima SQL pengguna yang tidak disetujui.
- **Alert 3** (`PostgresDialect.Explain`): `statement` berasal dari endpoint `EXPLAIN` yang (a) lolos *keyword allowlist* yang sama setelah perbaikan, (b) dijalankan di dalam **read-only transaction** — PostgreSQL menolak penulisan di sana (diverifikasi: `cannot execute INSERT in a read-only transaction`). `EXPLAIN ANALYZE` dengan efek samping juga tertahan oleh *transaction* yang sama. *False positive* praktis: **risiko diterima** dengan mitigasi berlapis.
- **Alert 5** (`MySQLDialect.Explain`): MySQL tidak memiliki *read-only transaction*, tetapi `MySQLReadableStatement` mengizinkan hanya statement non-mutasi sebelum *prefix* `EXPLAIN ` (diverifikasi: `EXPLAIN(UPDATE)` ditolak). `MultiStatements=false` pada driver menghalangi penggabungan statement. **False positive** praktis: mitigasi berlapis.
- `internal/schema/build` → `Diff`/`BuildSchema` murni transformasi data dalam memori dari hasil `information_schema`, tanpa SQL yang dibuat.

## Catatan

Hasil scan bersih **tidak** berarti aplikasi pasti aman; CodeQL menganalisis kode statis dengan *query* yang diketahui, bukan perilaku *runtime* (DAST) maupun salah konfigurasi infrastruktur.
