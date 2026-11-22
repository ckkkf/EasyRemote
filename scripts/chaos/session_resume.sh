#!/usr/bin/env bash
# ER-903c：真实中继进程重启后，会话级 rejoin（不重建密钥）。
# 需要本地 go/rust；stack 使用 development 内存装配。
set -euo pipefail
cd "$(dirname "$0")/../.."
OUT="docs/impl/evidence/ER-903c"
mkdir -p "$OUT"
export PATH="$(dirname "$(rustup which cargo)"):$PATH"

PORT=$(python3 - <<'PY'
import socket
s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()
PY
)
echo "stack port=$PORT"

(cd server && go build -o /tmp/er-stack ./cmd/stack && go build -o /tmp/er-relay-full ./cmd/relay)
ER_ENV=development /tmp/er-stack -addr "127.0.0.1:$PORT" > "$OUT/stack.log" 2>&1 &
STACK_PID=$!
trap 'kill $STACK_PID 2>/dev/null || true' EXIT
for i in $(seq 1 100); do
  if curl -fsS "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1; then break; fi
  sleep 0.1
done

(cd core && cargo run --release -p er-client --example session_resume_e2e -- "http://127.0.0.1:$PORT") | tee "$OUT/session-resume.log"
