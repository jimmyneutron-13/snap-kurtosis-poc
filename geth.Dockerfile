# Multi-stage build geth dari commit b05eff09 (commit yang diaudit K1 snap).
#
# Build context = root repo (berisi clone go-ethereum/ dan Dockerfile ini).
# Dipakai dengan:
#   docker build -f geth.Dockerfile -t poc-snap-geth:b05eff09 .
#
# Hasil: image minimal (alpine + binary geth) dengan ENTRYPOINT ["geth"],
# meniru layout image resmi ethereum/client-go — karena launcher
# ethereum-package menjalankan `geth init ... && geth ...` di container ini
# dengan cara yang sama seperti pada image resmi.
#
# Asumsi yang belum terverifikasi: launcher tidak butuh tool lain di dalam
# image EL selain `geth` (dan shell untuk `&&` — disediakan Kurtosis via
# entrypoint image/launcher). Bila build/run gagal karena tool hilang,
# tambahkan paket yang dibutuhkan di stage runtime.

# Perbaikan run #2 (19 Nov 2026): binary dibangun statis (CGO_ENABLED=0)
# karena binary glibc dari bookworm tidak bisa jalan di alpine (musl) —
# run #2 gagal "sh: geth: not found". Stage runtime menjalankan
# `geth version` agar docker build gagal cepat bila binary rusak.
#
# syntax=docker/dockerfile:1
FROM golang:1.25.4-bookworm AS builder
WORKDIR /src
COPY go-ethereum/ ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/geth ./cmd/geth

FROM alpine:3.20
COPY --from=builder /out/geth /usr/local/bin/geth
RUN geth version
ENTRYPOINT ["geth"]
