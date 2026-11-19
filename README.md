# Draf PoC Kurtosis GRATIS — EF K1 snap (GetStorageRanges CPU DoS)

Draf lokal untuk membuktikan K1 snap di devnet Kurtosis — kebutuhan checkbox
(1) form EF ("Kurtosis devnet atau state test") yang TIDAK dipenuhi PoC
2-node lokal. **Draf ini belum pernah dijalankan.** Semua yang ada di sini
hanya file lokal; tidak ada repo GitHub, tidak ada workflow run, tidak ada
biaya yang dikeluarkan.

Baca dulu: `RESEARCH.md` — terutama **risiko #1 (DISCLOSURE PUBLIK)** sebelum
membuat repo publik.

## File dalam draf ini

| File | Isi |
|---|---|
| `RESEARCH.md` | Temuan riset + sumber URL + terverifikasi vs asumsi + risiko/pertanyaan terbuka |
| `kurtosis-poc.yml` | Draf workflow GitHub Actions (letak final: `.github/workflows/kurtosis-poc.yml`) |
| `network-params.yaml` | Parameter ethereum-package: 1 partisipan geth (image kustom b05eff09) + lighthouse |
| `run-kurtosis-poc.sh` | Skrip runner: install → run devnet → tunggu blok → serangan → kumpulkan hasil |
| `geth.Dockerfile` | Build image geth dari commit b05eff09 (yang diaudit) |
| `cpu_docker.py` | Sampling CPU victim via `docker exec` + `/proc/<pid>/stat` |
| `attacker/main.go` | Attacker snap (diadaptasi dari PoC lokal: flag `-genesis-file`, `-enode`, `-host`) |
| `attacker/go.mod`, `attacker/go.sum` | Modul attacker; replace go-ethereum RELATIF (`../go-ethereum`) |

## Struktur repo saat dibuat (draf ini = root repo)

```
.
├── .github/workflows/kurtosis-poc.yml   <- dari kurtosis-poc.yml
├── network-params.yaml
├── run-kurtosis-poc.sh
├── geth.Dockerfile
├── cpu_docker.py
├── attacker/
│   ├── main.go
│   ├── go.mod
│   └── go.sum
└── go-ethereum/            <- DI-CLONE oleh workflow (shallow, commit b05eff09)
```

## Cara menjalankan (eksekusi — butuh keputusan pemilik dulu)

1. **Keputusan disclosure (wajib):** repo publik = kode PoC + log run + hasil
   terlihat siapa pun sebelum laporan EF. Lihat RESEARCH.md risiko #1.
   Tanpa persetujuan ini, jangan buat repo.
2. Buat repo **publik** baru, salin file draf ini ke root-nya (workflow ke
   `.github/workflows/kurtosis-poc.yml`), commit + push.
3. Buka tab **Actions** → workflow **kurtosis-poc-snap-k1** → **Run workflow**,
   isi `confirm_public_disclosure: true`, mode `full` (atau `smoke` untuk
   uji pipa: 10 blok + 10 akun).
4. Pantau run (~45–60 menit mode full). Artefak `kurtosis-poc-results`
   berisi `results-*.txt`, log victim + attack, dan `genesis.json`.
5. Hapus repo bila tidak dibutuhkan lagi (tetap: jendela publikasi selama
   run tidak bisa ditarik kembali — lihat risiko #1).

## Yang diharapkan dari run yang sukses

- Victim = geth commit b05eff09 di devnet Kurtosis, ≥130 blok, 130 diff layer
  (verifikasi baris `layers=N` di log shutdown EL).
- Serangan utama 100.000 akun bogus: **CPU victim multi-detik per request**
  (PoC lokal: 4,79 / 5,01 dtk; unit test: 5,935 dtk), 0 slot dikembalikan,
  dan biaya berskala hampir linear dengan jumlah akun (kontrol
  10 / 1.000 / 10.000).
- Bila angka jauh lebih kecil dari PoC lokal, pertama-tama periksa:
  diff-layer (apakah benar 130?), apakah ada service tambahan berjalan,
  dan apakah baseline idle bersih — sebelum menarik kesimpulan apa pun.

## Checklist eksekusi (saat run berjalan)

- [ ] Tag ethereum-package `@6.1.0` masih yang terbaru / masih ada?
- [ ] Kurtosis dari apt ≥ 1.23.0?
- [ ] Nama file di artefak `el-genesis-data` benar `genesis.json`?
- [ ] `--http.api` paket 6.x masih mencakup `admin` (probe `admin_nodeInfo`)?
- [ ] Service tambahan di enclave? (`kurtosis enclave inspect` — harus hanya EL+CL+VC)
- [ ] `layers=N` pada 130 blok (log shutdown EL)?
- [ ] Ukuran sebenarnya image engine + genesis-generator (`docker images`)?
- [ ] Ruang kosong awal runner (`df -h`)?
- [ ] Kontrol 10 akun sukses (handshake fork ID benar) SEBELUM 100k akun.

## Batas yang sudah diketahui

- Draf ini **belum pernah dijalankan** — belum ada yang terverifikasi di
  runner GitHub Actions. Yang terverifikasi: isi dokumen resmi, ukuran
  image dari Docker Hub, dan kompilasi attacker (lihat di bawah).
- `geth.Dockerfile` belum pernah di-build (tidak ada Docker di VM ini).
- Workflow belum pernah di-parse oleh GitHub (validasi lokal hanya
  `yaml.safe_load` + `bash -n`).

## Verifikasi lokal yang sudah dilakukan (19 Nov 2026)

- `gofmt -l attacker/main.go` → bersih.
- `bash -n run-kurtosis-poc.sh` → OK.
- `python3 -c yaml.safe_load` pada `kurtosis-poc.yml` + `network-params.yaml` → OK.
- `python3 -m py_compile cpu_docker.py` → OK.
- Uji kompilasi attacker di `/tmp` (salinan uji, replace diarahkan ke clone
  lokal, Go 1.25.4) → `go build` SUKSES. (Pemeriksaan kompilasi saja —
  BUKAN pemeriksaan jalan; tidak ada Kurtosis di VM ini.)
