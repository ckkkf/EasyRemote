#!/usr/bin/env bash
# ER-901 性能基准：原始输出保存到 docs/impl/evidence/ER-901/raw/。
# SOAK_SECONDS=1800 可跑 30min；默认 10s。
set -euo pipefail
cd "$(dirname "$0")/../.."
OUT="docs/impl/evidence/ER-901/raw"
mkdir -p "$OUT"
export PATH="$(dirname "$(rustup which cargo)"):$PATH"
SOAK="${SOAK_SECONDS:-10}"

echo "== build release examples =="
(cd core && cargo build --release -p er-codec --example media_capability -p er-codec --example bench_media -p er-transport --example transport_spike)

echo "== media capability =="
(cd core && ./target/release/examples/media_capability) | tee "$OUT/media_capability.json"

echo "== media benchmark (SOAK=${SOAK}s) =="
(cd core && ER_BENCH_SECONDS="$SOAK" ./target/release/examples/bench_media) | tee "$OUT/bench_media.json"

echo "== transport handshake/rtt =="
(cd core && ./target/release/examples/transport_spike) | tee "$OUT/transport_spike.log"

echo "== raw saved to $OUT =="
