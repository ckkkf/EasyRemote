#!/usr/bin/env bash
# ER-901a 30min soak：媒体编码/解码持续运行，每 5s 采一次 RSS。
# 用法：SOAK_SECONDS=1800 bash scripts/bench/soak.sh
set -euo pipefail
cd "$(dirname "$0")/../.."
OUT="docs/impl/evidence/ER-901a"
mkdir -p "$OUT"
export PATH="$(dirname "$(rustup which cargo)"):$PATH"
SOAK="${SOAK_SECONDS:-1800}"
BIN="core/target/release/examples/bench_media"

echo "== build =="
(cd core && cargo build --release -p er-codec --example bench_media)

echo "== soak ${SOAK}s =="
ER_BENCH_SECONDS="$SOAK" "$BIN" > "$OUT/media-soak.json" 2> "$OUT/media-soak.err" &
PID=$!
echo "pid=$PID start=$(date -u +%FT%TZ)" | tee "$OUT/rss.log"
while kill -0 "$PID" 2>/dev/null; do
  RSS=$(ps -o rss= -p "$PID" | tr -d ' ')
  echo "$(date +%s) ${RSS:-0}" >> "$OUT/rss.log"
  sleep 5
done
wait "$PID"
echo "== done =="
tail -1 "$OUT/media-soak.json"
