#!/usr/bin/env bash
# ER-903 可自动执行的故障/降级子集。PG/Redis/磁盘满/时钟偏差需外部环境，未包含。
set -euo pipefail
cd "$(dirname "$0")/../.."
OUT="docs/impl/evidence/ER-903"
mkdir -p "$OUT"

echo "== signal presence TTL / revoke =="
(cd server && go test ./internal/signal/ -run 'Presence|Revoke|ConsentNotifier|Bus' -count=1) | tee "$OUT/signal.log"

echo "== session timeout / expiry / reuse =="
(cd server && go test ./internal/session/ -run 'Timeout|Expire|Reuse|AuthWindow' -count=1) | tee "$OUT/session.log"

echo "== relay re-register / stale frame =="
export PATH="$(dirname "$(rustup which cargo)"):$PATH"
(cd core && cargo test -p er-session -- --nocapture) | tee "$OUT/relay.log"

echo "== lossy network e2e =="
(cd client && flutter test --concurrency=1 test/lossy_e2e_test.dart) | tee "$OUT/lossy.log"

echo "== done =="