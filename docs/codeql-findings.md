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

| Tanggal | Commit | Hasil | Link |
| :--- | :--- | :--- | :--- |
| 2026-11-27 | `02aa9b0` | (menunggu run selesai — diisi setelah scan hijau) | [run](https://github.com/PikaProgram/Orpbal/actions/workflows/codeql.yml) |

## Temuan

<!-- Diisi dari code-scanning alerts: rule | severity | lokasi | dampak | keputusan -->

## Keputusan perbaikan

<!-- Per temuan yang diperbaiki: referensi commit + hasil scan ulang. Temuan tidak diperbaiki: alasan (false positive / risiko diterima). -->

## Catatan

Hasil scan bersih **tidak** berarti aplikasi pasti aman; CodeQL menganalisis kode statis dengan query yang diketahui, bukan perilaku runtime (DAST) maupun salah konfigurasi infrastruktur.

Riwayat: sebelum 2026-11-27 semua run CodeQL gagal dengan *configuration error* — paket `internal/schema/build` tidak ter-commit karena aturan `.gitignore` root (`build` tanpa anchor) menelan direktori tersebut; build Go di runner gagal sehingga tidak ada database analisis. Diperbaiki di `29877e4` + `02aa9b0`.
