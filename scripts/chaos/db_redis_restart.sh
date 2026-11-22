#!/usr/bin/env bash
# ER-903d：独立 PG/Redis 容器上的进程级故障注入（不触碰本机已有服务）。
set -uo pipefail
cd "$(dirname "$0")/../.."
OUT="docs/impl/evidence/ER-903d"
mkdir -p "$OUT"
export PATH="$(dirname "$(rustup which cargo)"):$PATH"
SUFFIX=$$
PG_NAME="er-chaos-pg-$SUFFIX"
REDIS_NAME="er-chaos-redis-$SUFFIX"
PG_PORT=$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()')
REDIS_PORT=$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()')
STACK_PORT=$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()')
cleanup() {
  kill "$STACK_PID" 2>/dev/null || true
  docker rm -f "$PG_NAME" "$REDIS_NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker run -d --name "$PG_NAME" -e POSTGRES_PASSWORD=chaos -p "127.0.0.1:$PG_PORT:5432" postgres:17-alpine >/dev/null
docker run -d --name "$REDIS_NAME" -p "127.0.0.1:$REDIS_PORT:6379" redis:7-alpine >/dev/null
for i in $(seq 1 100); do docker exec "$PG_NAME" pg_isready -U postgres >/dev/null 2>&1 && break; sleep 0.2; done

(cd server && go build -o /tmp/er-stack-chaos ./cmd/stack)
DSN="postgres://postgres:chaos@127.0.0.1:$PG_PORT/postgres?sslmode=disable"
ER_ENV=development ER_ADMIN_TOTP_OFF=1 DATABASE_URL="$DSN" REDIS_URL="redis://127.0.0.1:$REDIS_PORT/0" \
  /tmp/er-stack-chaos -addr "127.0.0.1:$STACK_PORT" > "$OUT/stack.log" 2>&1 &
STACK_PID=$!
health() { curl -s -o /dev/null -w '%{http_code}' --max-time 2 "http://127.0.0.1:$STACK_PORT/healthz"; }
ready() { curl -s -o /dev/null -w '%{http_code}' --max-time 3 "http://127.0.0.1:$STACK_PORT/readyz"; }
dbprobe() {
  curl -s -o /dev/null -w '%{http_code}' --max-time 3 \
    -H "Authorization: Bearer $TOKEN" \
    "http://127.0.0.1:$STACK_PORT/admin/v1/stats/overview"
}
for i in $(seq 1 150); do [ "$(health)" = "200" ] && break; sleep 0.2; done

LOGIN_JSON=$(curl -s --max-time 5 -X POST -H 'Content-Type: application/json' \
  -d '{"username":"root","password":"dev-pass","totp":""}' \
  "http://127.0.0.1:$STACK_PORT/admin/v1/login")
echo "login=$LOGIN_JSON" | tee -a "$OUT/results.txt"
TOKEN=$(printf '%s' "$LOGIN_JSON" | python3 -c 'import json,sys
try: print(json.load(sys.stdin)["data"]["token"])
except Exception: print("")')


echo "start health=$(health) ready=$(ready) config=$(dbprobe)" | tee "$OUT/results.txt"

echo "== kill redis ==" | tee -a "$OUT/results.txt"
docker kill "$REDIS_NAME" >/dev/null
sleep 1
echo "health after redis kill=$(health) ready=$(ready) config=$(dbprobe)" | tee -a "$OUT/results.txt"

echo "== restart redis ==" | tee -a "$OUT/results.txt"
docker start "$REDIS_NAME" >/dev/null 2>&1 || true
sleep 1
echo "health after redis restart=$(health) ready=$(ready) config=$(dbprobe)" | tee -a "$OUT/results.txt"

# Redis 重启会丢掉 adminsession；重新登录后再测 PG。
LOGIN_JSON=$(curl -s --max-time 5 -X POST -H 'Content-Type: application/json' \
  -d '{"username":"root","password":"dev-pass","totp":""}' \
  "http://127.0.0.1:$STACK_PORT/admin/v1/login")
TOKEN=$(printf '%s' "$LOGIN_JSON" | python3 -c 'import json,sys
try: print(json.load(sys.stdin)["data"]["token"])
except Exception: print("")')
echo "relogin config=$(dbprobe)" | tee -a "$OUT/results.txt"

echo "== kill postgres ==" | tee -a "$OUT/results.txt"
docker kill "$PG_NAME" >/dev/null
sleep 1
echo "health after pg kill=$(health) ready=$(ready) config=$(dbprobe)" | tee -a "$OUT/results.txt"

echo "== restart postgres ==" | tee -a "$OUT/results.txt"
docker start "$PG_NAME" >/dev/null 2>&1 || true
for i in $(seq 1 100); do docker exec "$PG_NAME" pg_isready -U postgres >/dev/null 2>&1 && break; sleep 0.2; done
sleep 1
echo "health after pg restart=$(health) ready=$(ready) config=$(dbprobe)" | tee -a "$OUT/results.txt"