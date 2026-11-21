#!/usr/bin/env bash
# Ralph 冒烟：在临时端口起一个干净的 stack，验证关键接口，然后退出。
# 用法：bash scripts/ralph-smoke.sh
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT="${SMOKE_PORT:-8199}"
BIN=/tmp/er-ralph-stack

echo "[smoke] build server"
( cd "$ROOT/server" && go build -o "$BIN" ./cmd/stack ) || { echo "[smoke] BUILD FAIL"; exit 1; }

"$BIN" -addr ":$PORT" >/tmp/er-ralph-stack.log 2>&1 &
PID=$!
trap 'kill $PID 2>/dev/null' EXIT
sleep 1

pass() { echo "PASS  $1"; }
fail() { echo "FAIL  $1"; }
check() { # name, expected_substring, curl args...
  local name=$1 expect=$2; shift 2
  local body; body=$(curl -s "$@")
  if [[ "$body" == *"$expect"* ]]; then pass "$name"; else fail "$name -> $body"; fi
}

check "healthz" '"name":"OK"' "http://127.0.0.1:$PORT/healthz"

PHONE=13900009999
curl -s -X POST "http://127.0.0.1:$PORT/v1/auth/sms" -H 'content-type: application/json' -d "{\"phone\":\"$PHONE\"}" >/dev/null
TOK=$(curl -s -X POST "http://127.0.0.1:$PORT/v1/auth/login" -H 'content-type: application/json' -d "{\"phone\":\"$PHONE\",\"code\":\"123456\"}" | jq -r .token 2>/dev/null)
[[ -n "$TOK" && "$TOK" != "null" ]] && pass "login" || fail "login"

DEV=$(curl -s -X POST "http://127.0.0.1:$PORT/v1/devices/bind" -H 'content-type: application/json' -H "authorization: Bearer $TOK" -d '{"platform":"smoke"}')
CODE=$(echo "$DEV" | jq -r .raw_code 2>/dev/null)
DT=$(echo "$DEV" | jq -r .device_token 2>/dev/null)
[[ -n "$CODE" && "$CODE" != "null" ]] && pass "device bind" || fail "device bind"

# P0-1 文件接口（未实现时这里会是 FAIL，属于正常进度信号）
STATUS=$(curl -s -o /tmp/smoke_files.json -w '%{http_code}' "http://127.0.0.1:$PORT/v1/files" -H "authorization: Bearer $TOK" -H "x-device-token: $DT")
[[ "$STATUS" == "200" ]] && pass "files list" || fail "files list -> HTTP $STATUS"

# P0-3 协助码
AC=$(curl -s -X POST "http://127.0.0.1:$PORT/v1/assist/create" -H 'content-type: application/json' -H "authorization: Bearer $TOK" -H "x-device-token: $DT" -d "{\"device_code\":\"$CODE\"}" | jq -r .code 2>/dev/null)
[[ -n "$AC" && "$AC" != "null" ]] && pass "assist create -> $AC" || fail "assist create"

# P0-4 邮箱登录
E=$(curl -s -X POST "http://127.0.0.1:$PORT/v1/auth/email/code" -H 'content-type: application/json' -d '{"email":"ralph@example.com"}' | jq -r .ok 2>/dev/null)
[[ "$E" == "true" ]] && pass "email code" || fail "email code"

# P1-1 配置读取
CFG=$(curl -s "http://127.0.0.1:$PORT/v1/config" -H "authorization: Bearer $TOK" | jq -r '.config.session.max_minutes' 2>/dev/null)
[[ -n "$CFG" && "$CFG" != "null" ]] && pass "client config" || fail "client config"

# P1-3 出口地址
REF=$(curl -s "http://127.0.0.1:$PORT/v1/net/reflect" | jq -r .ip 2>/dev/null)
[[ -n "$REF" && "$REF" != "null" ]] && pass "net reflect -> $REF" || fail "net reflect"

# P0-2 前端依赖的四条文件路径：上传 → 列表 → 票据 → 下载 → 删除
printf 'hello easyremote\n' > /tmp/er-smoke.txt
UP=$(curl -s -X POST "http://127.0.0.1:$PORT/v1/files" -H "authorization: Bearer $TOK" -H "x-device-token: $DT" -F "file=@/tmp/er-smoke.txt")
FID=$(echo "$UP" | jq -r .file.id 2>/dev/null)
[[ -n "$FID" && "$FID" != "null" ]] && pass "files upload -> $FID" || fail "files upload -> $UP"
LISTED=$(curl -s "http://127.0.0.1:$PORT/v1/files" -H "authorization: Bearer $TOK" | jq -r '.files[0].name' 2>/dev/null)
[[ "$LISTED" == "er-smoke.txt" ]] && pass "files list name" || fail "files list name -> $LISTED"
TURL=$(curl -s -X POST "http://127.0.0.1:$PORT/v1/files/$FID/ticket" -H "authorization: Bearer $TOK" -H "x-device-token: $DT" | jq -r .url 2>/dev/null)
[[ "$TURL" == /v1/files/* ]] && pass "files ticket" || fail "files ticket -> $TURL"
curl -s "http://127.0.0.1:$PORT$TURL" -o /tmp/er-smoke-dl.txt   # 故意不带 Authorization：浏览器就是这条路
cmp -s /tmp/er-smoke.txt /tmp/er-smoke-dl.txt && pass "files download bytes" || fail "files download bytes"
curl -s -X DELETE "http://127.0.0.1:$PORT/v1/files/$FID" -H "authorization: Bearer $TOK" -H "x-device-token: $DT" >/dev/null
LEFT=$(curl -s "http://127.0.0.1:$PORT/v1/files" -H "authorization: Bearer $TOK" | jq -r '.files | length' 2>/dev/null)
[[ "$LEFT" == "0" ]] && pass "files delete" || fail "files delete -> $LEFT"

echo "[smoke] done (expect FAIL lines for not-yet-implemented tasks)"
