# Riset: PoC Kurtosis GRATIS untuk K1 snap (EF) — draf lokal

> **Status draf:** 19 Nov 2026 ~05:40 WIB. **BELUM DIEKSEKUSI, BELUM DIVERIFIKASI
> DENGAN RUN.** Semua yang ada di direktori ini adalah draf lokal — tidak ada
> repo GitHub yang dibuat, tidak ada workflow yang dijalankan, tidak ada uang
> yang keluar, tidak ada publikasi apa pun.
>
> **Bacaan wajib sebelum eksekusi:** bagian "Risiko #1 — disclosure publik"
> di bawah. Jalur yang didraf di sini (repo GitHub PUBLIK) membuat kode PoC +
> log + hasil run terlihat siapa pun di internet — itu pengungkapan publik atas
> kerentanan yang belum dilaporkan ke EF. Keputusan untuk memakai jalur ini
> adalah keputusan pemilik tugas, bukan rekomendasi draf ini.

## 0. Konteks singkat

- Temuan: `ServiceGetStorageRangesQuery` (`ethereum/go-ethereum` HEAD
  `b05eff09`, `eth/protocols/snap/handlers.go:172-310`) tidak membatasi jumlah
  akun per request. Satu peer devp2p tanpa autentikasi membakar ~5 dtk CPU
  victim per 100.000 akun bogus (terverifikasi: unit test 5,935 dtk in-process
  + PoC 2-node lokal 4,79/5,01 dtk CPU victim eksternal, 0 slot).
  Laporan: `../../reports/poc-snap-e2e-2026-11-19.md`.
- Blocker submit: form EF mewajibkan checkbox "Kurtosis devnet atau state
  test". PoC 2-node lokal BUKAN keduanya, jadi submit tertahan.
- Jalur gratis yang didraf: **GitHub Actions, repo publik** — runner standar
  gratis tanpa batas, 4 vCPU / 16 GB RAM / 14 GB SSD, Docker + Go, batas job
  6 jam. Workflow menjalankan devnet Kurtosis (ethereum-package,
  1 partisipan: geth EL + lighthouse CL), menunggu ~130 blok, lalu menjalankan
  attacker snap ke node geth devnet dan mengukur CPU victim dari luar.

## 1. Apa yang sudah dibaca (input lokal)

- `../../reports/poc-snap-e2e-2026-11-19.md` — dibaca penuh. Angka acuan:
  100k akun bogus = 4,79 dtk (r1) / 5,01 dtk (r2) CPU victim eksternal,
  wall attacker 4,13/4,19 dtk, 0 slot; kontrol 10 akun ≈ 0,01 dtk,
  1.000 ≈ 0,06 dtk, 10.000 ≈ 0,53 dtk; wire request 100k akun = 3.300.048 B
  (< 10 MB `maxMessageSize`); chain 130 blok, 130 diff layer terkonfirmasi
  (`layers=130` di log shutdown).
- `../poc-snap-e2e/attacker/main.go` (402 baris) — dibaca penuh.
  Input yang dibutuhkannya (dan adaptasi untuk Kurtosis — lihat §5):
  - `-rpc <url>` (HTTP-RPC victim): dipakai untuk `eth_chainId`,
    `eth_getBlockByNumber` (genesis `0x0` + `latest`), dan `admin_nodeInfo`
    (ambil pubkey dari URL `enode://<128-hex>@...`). Tanpa RPC ini attacker
    tidak bisa jalan.
  - `-port <n>` (port TCP devp2p victim): karena URL enode yang diiklankan
    victim berport 0 (`--nodiscover`/`--nat none`), port diambil dari flag,
    bukan dari enode.
  - `-accounts <n>`, `-mode bogus|real`, `-bytes` (soft limit, default
    2 MiB), `-timeout` (default 180 dtk).
  - Hardcode: victim di-dial di `127.0.0.1:<port>`; fork ID dihitung dari
    `params.AllDevChainProtocolChanges` (semua fork di 0 — benar HANYA untuk
    chain `--dev`); mode `real` memakai kunci dev geth yang well-known
    (`b71c71a6...`, faucet + precompile 0x01–0x09).
  - Output: semua baris diawali `POC `, siap dikutip verbatim.
- `../poc-snap-e2e/run-e2e.sh`, `cpu_time.py`, `mine-blocks.py` — dibaca.
  Pola yang dipakai ulang: baseline idle 10 dtk → serangan berurutan →
  CPU victim diukur dari LUAR proses victim (`/proc/<pid>/stat`,
  utime+stime, CLK_TCK=100) sebelum/sesudah tiap run → blok dicatat
  sebelum/sesudah tiap run.

## 2. Temuan riset (dengan sumber)

### 2.1 GitHub Actions — runner gratis, spesifikasi terverifikasi

- **Repo publik, runner standar `ubuntu-24.04`: 4 vCPU / 16 GB RAM /
  14 GB SSD, gratis dan tanpa batas; batas 1 job = 6 jam.**
  Sumber: GitHub Docs (via beberapa sumber sekunder yang mengutipnya,
  dibaca 19 Nov 2026) — `docs.github.com/en/actions/reference/limits`
  (batas job 6 jam), `docs.github.com/en/actions/reference/runners`
  (spesifikasi runner). Konfirmasi sekunder:
  <https://www.stephane-robert.info/en/docs/github-actions/runners/>,
  <https://sengi.run/blog/github-actions-free-minutes> ("Use of the standard
  GitHub-hosted runners is free and unlimited on public repositories",
  dibaca 13 Sep 2026 dari dok GitHub).
- **Runner `ubuntu-24.04` sudah terinstal Docker** (image 20260810:
  Docker 28.0.4, `gh` CLI 2.97.0) dan Go — sumber:
  <https://github.com/steamedfish/web888-debian/blob/HEAD/docs/dev/github-ci-apt-repo-research.md>
  ("Runner image (20260810.271.1) preinstalls: Docker 28.0.4").
  Go dipin eksplisit via `actions/setup-go@v5` (1.25.4) agar deterministik.
- **Repo privat TIDAK cocok untuk jalur ini** (bukan cuma soal biaya):
  runner standar repo privat hanya 2 vCPU / 8 GB RAM — di bawah kebutuhan
  devnet (butuh ~16 GB). Sumber: tabel yang sama di atas.
  Alternatif privat + larger runner (4 vCPU) berbayar dan statusnya
  terhadap "included usage" bertentangan antar sumber (lihat §6, risiko #2).
- **Artifact storage**: repo publik gratis (tidak ada kuota); repo privat
  Free 500 MB. Sumber: GitHub billing docs via sumber sekunder di atas.

### 2.2 Kurtosis CLI — install Linux x64, terverifikasi dari docs

- **Repo apt BARU** (Gemfury sudah TIDAK didukung):
  `echo "deb [trusted=yes] https://sdk.kurtosis.com/kurtosis-cli-release-artifacts/ /" | sudo tee /etc/apt/sources.list.d/kurtosis.list && sudo apt update && sudo apt install kurtosis-cli`
  Sumber: <https://docs.kurtosis.com/install/> dan
  <https://docs.kurtosis.com/upgrade/> (dibaca via hasil pencarian,
  crawl 1–2 hari lalu). Perintah lama `apt.fury.io/kurtosis-tech` sudah
  mati — JANGAN dipakai. Alternatif: `curl -sSL https://get.kurtosis.com/install.sh | sh`.
- **Matikan analytics untuk CI:** `kurtosis analytics disable`
  (perintah CLI, bukan env var). Sumber: docs Kurtosis "Configure
  analytics" + pola yang dipakai CI ethereum-package sendiri
  (`.github/actions/kurtosis-install/action.yaml` menjalankan
  `kurtosis analytics disable` — lihat commit
  `a00b6dd`, "fix: pull kurtosis images in kurtosis install (#1048)").
- **Versi minimum:** ethereum-package (main, Nov 2026) mensyaratkan
  **Kurtosis ≥ 1.23.0** (README Quickstart). Apt repo menginstal versi
  terbaru — cukup, tapi catat versi terinstal di log (`kurtosis version`).
- Perintah yang dipakai: `kurtosis run`, `kurtosis enclave inspect`,
  `kurtosis service inspect`, `kurtosis service logs`,
  `kurtosis service shell`, `kurtosis port print`, `kurtosis files download`,
  `kurtosis enclave rm`, `kurtosis clean`. Semua terverifikasi dari
  docs.kurtosis.com (CLI reference) dan/atau pemakaian nyata (lihat §2.4).

### 2.3 ethereum-package (ethpandaops) — parameter, terverifikasi dari README live

Repo: <https://github.com/ethpandaops/ethereum-package>.
README dibaca langsung (crawl <1 menit saat riset, 19 Nov 2026 05:35 UTC).
Rilis: **6.0.0 (5 Jan 2026)**; **6.1.0** dirujuk untuk konfigurasi
glamsterdam (skill erigon, Nov 2026). Draf ini mem-pin `@6.1.0` —
**verifikasi tag terbaru saat eksekusi** (bisa jadi sudah 6.1.x/6.2.x).

- Menjalankan: `kurtosis run --enclave <nama> github.com/ethpandaops/ethereum-package@<tag> --args-file network_params.yaml [--image-download missing]`
  (lihat §2.3.4 soal `--image-download`).
- **1 partisipan, geth + lighthouse:**
  ```yaml
  participants:
    - el_type: geth
      cl_type: lighthouse
  ```
  Nama service yang dihasilkan: `el-1-geth-lighthouse` (EL),
  `cl-1-lighthouse-geth` (CL) — terverifikasi dari panduan CherryServers
  (`el-1-geth-lighthouse`, `rpc: 8545/tcp -> 127.0.0.1:32769`).
- **Default `network_params` (README main, diverifikasi langsung):**

  | Parameter | Default | Yang dipakai draf |
  |---|---|---|
  | `network` | `"kurtosis"` | default |
  | `network_id` | `"3151908"` | default (attacker baca dari RPC, tidak di-hardcode) |
  | `seconds_per_slot` | 12 | **12** (aman; opsi 4 — lihat §2.3.3) |
  | `slot_duration_ms` | 12000 | default |
  | `genesis_delay` | 20 (detik) | default |
  | `genesis_time` | 0 (otomatis) | default |
  | `num_validator_keys_per_node` | 128 | **32** (lebih cepat, beban CL lebih kecil) |
  | `altair/bellatrix/capella/deneb/electra/fulu_fork_epoch` | **0 semua** | 0 eksplisit (semua fork aktif sejak genesis) |
  | `gloas_fork_epoch`, `heze_fork_epoch` | 18446744073709551615 (dimatikan) | default (jangan diaktifkan) |
  | `preset` | `"mainnet"` | default |
  | `prefunded_accounts` | `{}` (tidak ada) | default (mode `real` attacker TIDAK dipakai) |
  | `max_payload_size` | 10485760 (10 MiB) | default (cocok dengan `maxMessageSize` geth) |
  | `genesis_gaslimit` | 60000000 | default |
  | `preregistered_validator_keys_mnemonic` | `"giant issue aisle ... have athlete"` (mnemonic test publik) | default |

- **Batas resource per partisipan (README, diverifikasi):**
  `el_min_cpu / el_max_cpu` (millicore), `el_min_mem / el_max_mem` (MB),
  `el_volume_size` (MB), dan padanannya `cl_*` / `vc_*`. Default 0 =
  tanpa batas. Draf memakai:
  EL `min_cpu 500 / max_cpu 2000 / min_mem 512 / max_mem 4096`,
  CL `min 200 / max 1000 / min_mem 256 / max_mem 2048`,
  VC `min 100 / max 500 / min_mem 128 / max_mem 1024`.
  Total plafon ~3.500 millicore + engine Kurtosis < 4.000 millicore runner.
  Catatan: CPU-seconds yang diukur TIDAK rusak oleh throttling (kerja yang
  sama tetap menghabiskan CPU-seconds yang sama, hanya wall time memanjang).
  Nilai ini dapat di-tuning saat eksekusi.
- **Override image EL (`el_image`) didukung** (string kosong = default
  client). Draf memakai image geth kustom dari commit `b05eff09`
  (`poc-snap-geth:b05eff09`, di-build di workflow — lihat §4).
  `cl_image` dibiarkan default paket (jangan override tanpa alasan).
- **Genesis EL bisa diunduh:** `kurtosis files download <enclave> el-genesis-data <dir>`
  (README bagian Debugging, diverifikasi langsung). Artefak ini berisi
  `genesis.json` geth — INI KUNCI untuk adaptasi fork ID attacker (§5).
  Alternatif: additional service `nginx` menyajikan `/network-config.tar`
  (berisi `genesis.json` + `enodes.txt`) — tidak dipakai, `files download`
  lebih sederhana.
- **Port service EL (terverifikasi dari log `kurtosis run` nyata):**
  `engine-rpc` 8551, `rpc` 8545, `tcp-discovery` 30303,
  `udp-discovery` 30303, `ws` 8546. Sumber: issue kurtosis-tech/kurtosis
  #928 (log peluncuran geth launcher). **HTTP API yang diaktifkan
  launcher: `--http.api=admin,engine,net,eth`** — artinya
  `admin_nodeInfo` TERSEDIA via RPC (kecuali paket 6.x mengubah daftar
  ini; rilis 6.0.0 mencatat "Add 'trace' to enabled JSON-RPC modules",
  jadi daftar itu masih dikelola eksplisit — verifikasi ulang saat eksekusi
  dengan probe `admin_nodeInfo` satu baris; fallback ada di §5).
  Catatan: log issue #928 itu dari lineage launcher lama (2023); launcher
  ethereum-package 6.x adalah keturunannya. Tingkat keyakinan: tinggi,
  tapi tetap butuh probe saat eksekusi.

#### 2.3.1 Waktu menuju ~130 blok

- Slot 12 detik → 130 blok ≈ **26 menit** (plus genesis generation +
  startup EL/CL ~3–8 menit; total workflow diperkirakan **45–60 menit**,
  jauh di bawah batas job 6 jam).
- `seconds_per_slot` bisa diturunkan: ethpandaops blog mencontohkan 6;
  demo nyata (evm-breaker, commit `7974271`) memakai **`seconds_per_slot: 4`**
  dengan `electra_fork_epoch: 0` dan berhasil. 130 blok pada 4 dtk/slot
  ≈ 9 menit. Draf memakai **12 (default paket, paling aman)**; opsi 4
  dicatat sebagai percepatan yang boleh dicoba BILA run 12 dtk kehabisan
  waktu (kemungkinan kecil) — dengan catatan slot 4 dtk memberi CL lebih
  sedikit waktu per slot.
- Blok diproduksi hanya bila ada proposer aktif; dengan 1 node + 32
  validator, hampir semua slot terisi (missed slot sesekali tidak masalah
  — target dihitung dari `eth_blockNumber`, bukan dari waktu).

#### 2.3.2 Berapa diff layer pada 130 blok Kurtosis?

- Di PoC lokal, 130 blok `--dev` → 130 diff layer (`layers=130` di log
  shutdown geth). Pada devnet Kurtosis, EL menerima blok dari CL satu per
  slot; snapshot pathdb menumpuk diff layer per blok dengan cara yang sama
  (launcher memakai `--syncmode=full`; gcmode default). **Asumsi yang
  masih harus dibuktikan saat eksekusi:** 130 blok Kurtosis ≈ 130 diff
  layer. Cara membuktikannya: hentikan service EL di akhir run
  (`kurtosis service stop`) lalu grep log-nya untuk baris
  `Persisting dirty state ... layers=N` (cara yang sama seperti PoC lokal).
  Bila layer jauh di bawah 130 (mis. karena capping), naikkan target blok.
  Angka CPU per akun tetap valid apa pun layer-nya; yang berubah hanya
  perbandingan 1:1 dengan PoC lokal.

#### 2.3.3 `--image-download`: HARUS `missing`, bukan `always`

- Contoh ethpandaops memakai `--image-download always`. Untuk kita itu
  **SALAH**: image geth kustom (`poc-snap-geth:b05eff09`) hanya ada lokal
  di runner; `always` akan mencoba pull dari registry dan gagal.
  Pakai `--image-download missing` (pull hanya yang belum ada lokal).
  Pola ini sama dengan yang dipakai CI erigon untuk image lokal
  (`test/erigon:current`).

### 2.4 Cara membaca enode + port + CPU dari luar

- **Port host:** `kurtosis port print <enclave> <service> <port-id>`
  mencetak URL host, mis. `http://127.0.0.1:32769`
  (sumber: docs Kurtosis `port print`; contoh nyata di
  docs.gateway.fm dan skill erigon:
  `kurtosis port print "$ENCLAVE" "$EL_SERVICE" rpc`).
  Yang dibutuhkan: `rpc` (8545) dan `tcp-discovery` (30303).
  Output di-parse robust (buang prefix `http://`, ambil port setelah `:`).
- **Service:** `kurtosis enclave inspect <enclave>` (daftar service +
  port mapping) dan `kurtosis service inspect <enclave> <service>`
  (detail + UUID lengkap dengan `--full-uuid`). Nama service EL
  deterministik: `el-1-geth-lighthouse`.
- **Enode victim:** utama via RPC `admin_nodeInfo` (HTTP API mencakup
  `admin`, §2.3); fallback: grep `enode://` dari
  `kurtosis service logs <enclave> el-1-geth-lighthouse` (geth mencatat
  enode saat "Started P2P networking"). Attacker hanya butuh pubkey dari
  enode (IP/port di URL diabaikan — dial ke `127.0.0.1:<port host>`).
- **CPU victim dari luar — metode utama (sama seperti PoC lokal):**
  `docker exec <container-el> cat /proc/<pid-geth>/stat` → utime+stime
  (CLK_TCK=100). Container ID dicari dari `docker ps` lewat UUID service
  (`kurtosis service inspect --full-uuid`); PID geth dicari di dalam
  container via `docker exec <cid> pgrep -x geth` (PID 1 di container
  belum tentu geth — entrypoint launcher menjalankan
  `geth init ... && geth ...`, jadi PID harus dicari, jangan diasumsikan).
  Skrip: `cpu_docker.py` (di direktori ini).
  **Fallback:** `docker stats --no-stream` (CPU% sesaat per container) —
  kurang presisi untuk CPU-seconds; hanya dipakai bila metode utama gagal,
  dan hasilnya ditandai sebagai estimasi.
- **Log victim:** `kurtosis service logs <enclave> el-1-geth-lighthouse`
  (tambahkan `--follow` bila perlu). Di akhir run, hentikan EL
  (`kurtosis service stop`) lalu ambil log untuk baris `layers=N`.

### 2.5 Ukuran image (download) vs SSD 14 GB

Angka compressed (terverifikasi dari Docker Hub, crawl ≤2 hari lalu):

| Image | Tag acuan | Compressed (linux/amd64) |
|---|---|---|
| `ethereum/client-go` | `latest` | **28,18 MB** |
| `ethereum/client-go` | `stable` | 23,93 MB |
| `sigp/lighthouse` | `v8.2.2` | **63,61 MB** |
| `sigp/lighthouse` | `latest` | 63,6 MB |

Belum terverifikasi (estimasi, ditandai): image engine Kurtosis
(`kurtosistech/engine:<versi>`, perkiraan 200–500 MB),
`ethpandaops/ethereum-genesis-generator:<versi>` (perkiraan 300 MB–1 GB),
image builder Go `golang:1.25.4-bookworm` (~800 MB uncompressed,
~300 MB compressed), source go-ethereum shallow clone (~100–200 MB),
modul Go + build cache (beberapa ratus MB, dibatasi cache mount BuildKit).
Perkiraan total uncompressed di disk: **~3–5 GB** — muat di ~10 GB
ruang kosong runner, tapi ini resource paling ketat. Workflow mencatat
`df -h` + `docker system df` di tiap fase dan gagal cepat (fail fast)
bila ruang kosong < 8 GB sebelum `kurtosis run`. Langkah pembebasan disk
(di workflow) menghapus tool cache besar yang tidak dipakai
(`/usr/share/dotnet`, `/opt/ghc`, dst.) — pola standar untuk runner
ubuntu yang kehabisan disk.

### 2.6 Preseden: pola ini sudah dipakai orang lain di CI

- **ethereum-package CI sendiri** menjalankan Kurtosis di GitHub Actions
  (`.github/actions/kurtosis-install`, `.github/tests/*.yaml`) —
  termasuk `kurtosis analytics disable` + pull image engine paralel.
- **`ethpandaops/kurtosis-assertoor-github-action@v1`** — action komposit
  yang menginstal Kurtosis, menjalankan ethereum-package dari file args,
  dan memantau assertoor. Dipakai luas di CI client Ethereum. Draf ini
  TIDAK memakainya (menambah container assertoor = beban CPU/RAM/disk
  ekstra + kami tidak butuh assertoor) — kami memakai CLI mentah.
  Dicatat sebagai alternatif bila CLI mentah bermasalah.
- **Erigon CI** (`.github/workflows/test-kurtosis-assertoor.yml`) —
  membangun image EL lokal lalu menjalankan ethereum-package dengannya di
  GH Actions; pola yang sama dengan draf ini.
- **evm-breaker** (catwith1hat, commit `7974271`) — demo fork Kurtosis
  dengan `seconds_per_slot: 4` + parsing `kurtosis service inspect`
  untuk RPC URL; pola parsing port yang dipakai ulang di skrip kami.

### 2.7 GitHub Actions — fakta biaya untuk alternatif repo privat

- Repo publik: gratis tanpa batas (runner standar). Repo privat Free:
  tunjangan berubah-ubah menurut sumber (model lama 2.000 menit/bulan;
  model baru Nov 2026: $12/bulan included usage — sengi.run, 7 Nov 2026).
  Tarif Linux 2-core: $0,008/menit (lama) → **$0,006/menit sejak
  1 Jan 2026** (hivebook.wiki, terverifikasi 3 Sep 2026 terhadap docs
  GitHub) + ada platform charge $0,002/menit (stackpick, Okt 2026).
  Sumber-sumber ini **saling bertentangan sebagian** — angka privat
  dicatat sebagai perkiraan kasar saja, dan repo privat tetap TIDAK
  memenuhi syarat hardware (2 vCPU/8 GB, §2.1).

## 3. Keputusan desain draf

1. **Victim = geth kustom dari commit `b05eff09`** (bukan image release
   paket). Alasan: temuan diaudit terhadap commit itu persis; image
   release (`ethereum/client-go:v1.17.x`) kemungkinan besar mengandung bug
   yang sama, tapi "kemungkinan besar" bukan bukti. Image di-build di
   workflow dari source (multi-stage Dockerfile, `geth.Dockerfile`).
   CL = image default paket (lighthouse) — CL bukan target.
2. **Satu partisipan saja.** Tidak butuh 2 node: attacker adalah peer
   eksternal (proses di runner host), bukan node ke-2 di enclave.
3. **Fork epoch semua 0** (default paket) + **attacker membaca config
   dari `genesis.json` asli** (§5). Ini satu-satunya cara yang robust
   untuk fork ID.
4. **Tidak ada additional service** (`additional_services: []`) — tanpa
   spammer, Grafana, Prometheus, dora, assertoor. Alasan: beban CPU
   latar belakang merusak atribusi pengukuran CPU victim. (Catatan:
   README intro menyebut spammer/monitoring sebagai hal yang dilakukan
   paket — belum terverifikasi apakah ada yang aktif secara default;
   draf menetapkan `[]` eksplisit dan workflow memeriksa
   `kurtosis enclave inspect` untuk service tak terduga.)
5. **Mode `real` attacker TIDAK dipakai** di run Kurtosis. Alasan:
   kunci faucet dev (`--dev`) tidak ada di chain Kurtosis, dan kunci
   prefunded paket (21 kunci, README §"Pre-funded accounts at Genesis")
   tidak tercantum lengkap dalam teks README yang dibaca. Kontrol bogus
   (10 / 1.000 / 10.000 akun) sudah cukup membuktikan biaya berskala
   dengan jumlah akun — itu fungsi kontrol `real` di PoC lokal
   (10 real ≈ 0,01 dtk, sama dengan 10 bogus).
6. **Pengukuran CPU = `/proc/<pid>/stat` via `docker exec`** (metode
   utama, satuan sama dengan PoC lokal: CPU-seconds). `docker stats`
   hanya fallback.
7. **Workflow hanya `workflow_dispatch`** (tidak ada trigger push/PR)
   + input konfirmasi disclosure (lihat §6) + `permissions: contents: read`
   + `timeout-minutes: 300` (di bawah batas keras 6 jam).

## 4. File draf di direktori ini

| File | Isi |
|---|---|
| `RESEARCH.md` | Dokumen ini. |
| `README.md` | Cara pakai draf + checklist eksekusi. |
| `kurtosis-poc.yml` | Draf workflow GitHub Actions (disimpan di `.github/workflows/` saat repo dibuat). |
| `network-params.yaml` | Parameter ethereum-package (1 partisipan geth+lighthouse, batas resource, fork epoch 0). |
| `run-kurtosis-poc.sh` | Skrip runner: install Kurtosis → jalankan package → tunggu blok → build attacker → serangan → kumpulkan artefak. Dipanggil workflow; bisa juga jalan mandiri di mesin Linux ber-Docker. |
| `geth.Dockerfile` | Multi-stage build geth dari `b05eff09`. |
| `cpu_docker.py` | Sampling CPU container EL via `docker exec` + `/proc/<pid>/stat`. |
| `attacker/main.go` | Attacker PoC lokal + adaptasi Kurtosis (flag `-genesis-file`, `-enode`, `-host`). |
| `attacker/go.mod` | `replace` diarahkan relatif (`../go-ethereum`). |
| `attacker/go.sum` | Disalin dari PoC lokal (dependensi sama). |

Yang TIDAK ada di sini (sengaja, dibuat saat eksekusi): clone
`go-ethereum@b05eff09` (dibuat workflow di `$GITHUB_WORKSPACE/go-ethereum`),
binary `geth`/`snap-attacker`, enclave Kurtosis, hasil run.

## 5. Adaptasi attacker untuk Kurtosis (detail)

Masalah terbesar yang ditemukan riset ini: **fork ID**.

- Attacker lokal menghitung fork ID dari `params.AllDevChainProtocolChanges`
  (semua fork di 0 — benar untuk chain `--dev`). Chain Kurtosis memakai
  jadwal fork nyata: fork berbasis waktu (Shanghai/Cancun/Prague/Osaka)
  = **genesis time** (bukan 0), dan fork yang dimatikan paket (Gloas, Heze,
  dan kemungkinan BPO) **tidak ada** di config. Bila attacker memakai
  `AllDevChainProtocolChanges`, fork ID-nya salah → geth victim menolak
  handshake eth (validasi fork ID EIP-2124) → serangan gagal sebelum
  dimulai, dengan log yang mudah salah dibaca sebagai "bug tidak
  reproduce".
- **Solusi yang didraf (robust):** attacker menerima flag baru
  `-genesis-file <path>`; runner mengunduh `el-genesis-data` dari enclave
  (`kurtosis files download`) dan mengoper path `genesis.json`-nya.
  Attacker melakukan `json.Unmarshal` ke `core.Genesis` dan memakai
  `genesis.Config` (config persis seperti yang dipakai victim — karena
  genesis yang sama dipakai `geth init` di container) untuk
  `forkid.NewID`. ChainID dari config dicocokkan dengan `eth_chainId`
  dari RPC; bila tidak cocok, attacker gagal cepat dengan pesan jelas.
  Fallback bila flag tidak diisi: perilaku lama (`AllDevChainProtocolChanges`)
  + baris log yang menyatakan fallback itu dipakai.
- **Enode via flag:** `-enode <url>` baru; bila diisi, pubkey diparse
  dari URL dan panggilan `admin_nodeInfo` dilewati. Runner mengisi flag
  ini dari `admin_nodeInfo` (utama) atau dari grep `enode://` pada
  `kurtosis service logs` (fallback). Bila flag kosong, attacker memakai
  `admin_nodeInfo` seperti sekarang.
- **Host dial:** flag `-host` baru (default `127.0.0.1`); port tetap dari
  `-port` (port host hasil `kurtosis port print ... tcp-discovery`).
- **`go.mod`:** `replace` absolut lokal
  (`=> /home/hatch/workspace/bug-bounty/work/go-ethereum`) diganti relatif
  (`=> ../go-ethereum`). Workflow meng-clone go-ethereum@`b05eff09`
  ke `$GITHUB_WORKSPACE/go-ethereum` sebelum build attacker.
- **Mode `real`:** tidak dipakai di Kurtosis (lihat §3.5). Kode mode
  `real` tetap ada di attacker (tidak dihapus) tapi tidak dipanggil
  oleh skrip runner.
- Attacker hasil adaptasi sudah diperiksa kompilasinya di VM ini
  (salinan uji di `/tmp`, `replace` diarahkan sementara ke clone lokal,
  Go 1.25.4 — lihat `README.md`). Itu pemeriksaan kompilasi saja, BUKAN
  pemeriksaan jalan (tidak ada Kurtosis di VM ini).

## 6. Risiko & pertanyaan terbuka

### Risiko #1 — DISCLOSURE PUBLIK (paling penting, belum diputuskan)

- Repo **publik** berarti: kode attacker + workflow + parameter + **log
  run + artefak hasil** (yang berisi bukti exploit terukur, enode victim,
  langkah reproduksi persis) terlihat siapa pun di internet sejak repo/run
  dibuat — **sebelum EF menerima laporan**.
- Ini pengungkapan publik atas kerentanan yang belum ditambal dan belum
  dilaporkan. Bertentangan dengan disclosure policy pada umumnya dan
  dengan panduan yang baru dicatat di `programs.md` (Airdropfinds:
  "patuhi disclosure policy, jangan publikasi sebelum diizinin").
  Bug ini "hanya" DoS (tanpa kehilangan dana) dan PoC menargetkan devnet
  privat (bukan mainnet) — dampaknya terbatas, tapi tetap publikasi
  exploit sebelum laporan.
- Mitigasi yang dipertimbangkan (tidak ada yang bersih):
  - (a) Repo privat + larger runner 4 vCPU: hardware cocok, tapi
    status biaya terhadap included usage bertentangan antar sumber
    (§2.7) dan runner standar privat (2 vCPU/8 GB) tidak cukup.
    Kemungkinan biaya ~$0,84/run (60 menit × ~$0,014/menit) — perkiraan,
    belum terverifikasi.
  - (b) Repo publik tapi attacker diambil saat runtime dari repo privat
    via token: log run tetap publik dan tetap menunjukkan hasil serangan
    — tidak menyelesaikan disclosure hasil.
  - (c) Hapus repo segera setelah run selesai: jendela publikasi
    45–60 menit tetap ada; log mungkin sudah ter-cache.
  - (d) Mesin gratis lain (mis. free tier cloud, PC sendiri): di luar
    draf ini.
- **Draf ini ditulis sesuai rencana yang diminta (repo publik), tapi
  workflow diberi guard:** trigger hanya `workflow_dispatch` + input
  wajib `confirm_public_disclosure` (harus `true`, kalau tidak job
  gagal di langkah pertama) + peringatan disclosure dicetak di log.
  Keputusan akhir tetap di pemilik tugas. **Jangan menjalankan workflow
  ini tanpa memahami risiko ini.**

### Risiko #2 — Biaya repo privat (bila dipilih sebagai mitigasi)

- Sumber bertentangan: model lama 2.000 menit/bulan vs model baru
  $12/bulan (Nov 2026); tarif Linux 2-core $0,008 → $0,006/menit
  (1 Jan 2026) + platform charge $0,002/menit; larger runner mungkin
  dikecualikan dari included usage. Verifikasi di halaman billing
  GitHub saat eksekusi bila jalur ini dipilih.

### Risiko #3 — Disk 14 GB (paling ketat secara teknis)

- Total unduhan diperkirakan ~1,5–2,5 GB compressed, ~3–5 GB
  uncompressed (engine Kurtosis + genesis-generator belum terverifikasi
  ukurannya). Ruang kosong runner segar biasanya ~10 GB.
  Mitigasi di workflow: `df -h` + `docker system df` tiap fase,
  fail fast bila < 8 GB sebelum `kurtosis run`, hapus tool cache besar
  yang tidak dipakai, `docker image prune` setelah build image geth.
  Bila tetap kehabisan disk: turunkan `el_volume_size`/`cl_volume_size`
  (default paket) atau hapus image builder setelah build.

### Risiko #4 — Fork ID / handshake (risiko teknis utama)

- Bila `-genesis-file` tidak terbaca atau `genesis.json` di artefak
  `el-genesis-data` bernama lain, attacker gagal atau memakai fallback
  yang salah. Mitigasi: runner memverifikasi isi artefak
  (`ls` + `python3 -c` baca `config.chainId`) sebelum serangan;
  serangan pertama adalah kontrol kecil (10 akun) yang gagal cepat
  dengan log jelas bila handshake ditolak. Jangan lanjut ke 100k akun
  sebelum kontrol 10 akun sukses.

### Risiko #5 — Waktu startup & kestabilan devnet

- Genesis generation + startup bisa lebih lama dari perkiraan bila
  runner lambat; 130 blok pada 12 dtk/slot = 26 menit; total ~45–60
  menit. Masih jauh di bawah 6 jam, tapi bila CL tertinggal/restart,
  `eth_blockNumber` bisa macet — workflow memakai timeout 60 menit
  untuk menunggu blok dan mengunggah log apa pun hasilnya (`if: always()`).
- `seconds_per_slot: 4` (dipakai demo evm-breaker) adalah percepatan
  yang tersedia bila diperlukan; belum diuji dengan konfigurasi ini.

### Risiko #6 — Jumlah diff layer di devnet Kurtosis

- Belum terbukti 130 blok Kurtosis = 130 diff layer (lihat §2.3.2).
  Bila layer jauh lebih sedikit, angka CPU per akun akan lebih kecil
  dari PoC lokal — hasil tetap valid sebagai bukti (biaya > 0 dan
  berskala), tapi perbandingan 1:1 dengan PoC lokal melemah. Mitigasi:
  verifikasi `layers=N` dari log shutdown EL; bila perlu naikkan
  target blok.

### Risiko #7 — Port & service tidak sesuai harapan

- Nama service (`el-1-geth-lighthouse`) dan nama port (`rpc`,
  `tcp-discovery`) terverifikasi dari sumber nyata, tapi paket 6.x bisa
  mengubahnya. Mitigasi: runner tidak meng-hardcode buta — ia
  memverifikasi lewat `kurtosis enclave inspect` dan gagal dengan pesan
  jelas bila service/port tidak ditemukan.

### Risiko #8 — Docker Hub rate limit (anonim)

- Runner GH menarik image publik tanpa login; Docker Hub membatasi pull
  anonim (100 pull/6 jam per IP, dibagi dengan runner lain).
  ethereum-package CI menarik image engine via mirror
  (`docker.ethquokkaops.io/dh/...`) sebagian untuk alasan ini.
  Mitigasi: workflow menarik image engine Kurtosis eksplisit di awal
  (mengikuti pola CI paket); bila pull gagal karena rate limit, job
  gagal cepat dengan pesan jelas — coba lagi beberapa jam kemudian.
  (Tidak ada login Docker Hub yang dipakai; draf ini tidak membuat akun
  apa pun.)

### Risiko #9 — Noise CPU dari service lain

- Bila paket ternyata menjalankan spammer/monitoring secara default,
  CPU latar belakang merusak atribusi. Mitigasi: `additional_services: []`
  eksplisit + periksa `kurtosis enclave inspect` + baseline idle 10 dtk
  sebelum serangan (seperti PoC lokal) sehingga noise terlihat.

### Pertanyaan terbuka (cek saat eksekusi)

1. Tag terbaru ethereum-package (draf mem-pin `@6.1.0`)?
2. Versi Kurtosis yang terinstal dari apt (harus ≥ 1.23.0)?
3. Nama file persis di artefak `el-genesis-data` (apakah `genesis.json`)?
4. Apakah `--http.api` paket 6.x masih mencakup `admin` (probe
   `admin_nodeInfo`)?
5. Apakah ada service tambahan yang berjalan di luar yang diminta
   (`kurtosis enclave inspect`)?
6. Berapa `layers=N` pada 130 blok (log shutdown EL)?
7. Berapa ukuran sebenarnya image engine + genesis-generator
   (`docker images` setelah pull)?
8. Berapa ruang kosong awal runner (`df -h`)?

## 7. Perkiraan waktu & biaya (draf)

| Fase | Perkiraan |
|---|---|
| Setup runner (checkout, Go, Kurtosis, free disk) | ~3–5 mnt |
| Clone go-ethereum@`b05eff09` (shallow) + build image geth | ~5–12 mnt |
| Build attacker | ~1–3 mnt |
| `kurtosis run` (genesis + EL/CL/VC start) | ~3–8 mnt |
| Tunggu 130 blok @ 12 dtk/slot | ~26 mnt |
| Baseline + serangan (kontrol + 2× 100k) | ~3–5 mnt |
| Kumpulkan log + artefak + cleanup | ~2–3 mnt |
| **Total** | **~45–60 mnt** |
| **Biaya** | **$0 (repo publik, runner standar)** — dengan catatan risiko #1 |

## 8. Sumber

- Kurtosis install/upgrade: <https://docs.kurtosis.com/install/>,
  <https://docs.kurtosis.com/upgrade/>
- Kurtosis CLI reference (`port print`, `service inspect`, `enclave inspect`):
  <https://docs.kurtosis.com/> (via hasil pencarian)
- ethereum-package README (dibaca langsung, main, 19 Nov 2026):
  <https://github.com/ethpandaops/ethereum-package>
- ethereum-package CI install action (commit `a00b6dd`):
  <https://github.com/ethpandaops/ethereum-package/commit/a00b6ddb10b0232a4d674b8b8b7b65ad5ff49e2f>
- ethPandaOps "Kurtosis: A Deep Dive": <https://ethpandaops.io/posts/kurtosis-deep-dive/>
- Kurtosis + assertoor GitHub Actions: <https://ethpandaops.io/posts/assertoor-introduction/>
- Erigon kurtosis-test skill (pola CI + `port print`):
  <https://github.com/lrifton92/erigon/blob/HEAD/.claude/skills/kurtosis-test/SKILL.md>
- evm-breaker kurtosis fork demo (`seconds_per_slot: 4`, parsing
  `service inspect`): <https://github.com/catwith1hat/evm-breaker/commit/79742718f9f309dd5c55726ae8bee563f80ac811>
- Geth launcher ports + `--http.api=admin,engine,net,eth` (log nyata):
  <https://github.com/kurtosis-tech/kurtosis/issues/928>
- Docker Hub ukuran image: <https://hub.docker.com/r/ethereum/client-go/tags>,
  <https://hub.docker.com/r/sigp/lighthouse/tags>
- GitHub Actions runner specs & limits:
  <https://www.stephane-robert.info/en/docs/github-actions/runners/>,
  <https://sengi.run/blog/github-actions-free-minutes>,
  <https://github.com/steamedfish/web888-debian/blob/HEAD/docs/dev/github-ci-apt-repo-research.md>
- GitHub Actions billing (model baru + tarif 2026):
  <https://www.hivebook.wiki/wiki/github-actions-limits>,
  <https://www.stackpick.net/tools/github-actions/>
- PoC lokal (input): `../../reports/poc-snap-e2e-2026-11-19.md`,
  `../poc-snap-e2e/attacker/main.go`, `../poc-snap-e2e/run-e2e.sh`
