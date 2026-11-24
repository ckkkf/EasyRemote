#!/usr/bin/env bash
# ER-905 跨进程 E2E：A 主控断开 / B 被控断开 / C 拒绝（真实双进程）。
#
# 约束：端口随机；只清理本脚本自己启动的 PID（绝不 pkill）；日志脱敏（allowlist 生成 timeline）。
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT="${OUT:-/tmp/er905-e2e}"
mkdir -p "$OUT"

( cd "$ROOT/server" && go build -o /tmp/er905-stack ./cmd/stack && go build -o /tmp/er905-relay ./cmd/relay ) || { echo 'E2E_FAIL: build'; exit 1; }

TMP_HOME="$(mktemp -d)"
STACK_PID=""; RELAY_PID=""; HOST_PID=""
cleanup() {
  [ -n "$HOST_PID" ] && { kill "$HOST_PID" 2>/dev/null; wait "$HOST_PID" 2>/dev/null; }
  [ -n "$RELAY_PID" ] && { kill "$RELAY_PID" 2>/dev/null; wait "$RELAY_PID" 2>/dev/null; }
  [ -n "$STACK_PID" ] && { kill "$STACK_PID" 2>/dev/null; wait "$STACK_PID" 2>/dev/null; }
  rm -rf "$TMP_HOME" 2>/dev/null
}
trap cleanup EXIT INT TERM
fail() { echo "E2E_FAIL: $*"; exit 1; }
free_port() { python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()'; }
wait_for() { for _ in $(seq 1 "$2"); do curl -fsS -m 1 -o /dev/null "$1" && return 0; sleep 0.2; done; return 1; }

GW_PORT="$(free_port)"; RELAY_PORT="$(free_port)"
GW="http://127.0.0.1:$GW_PORT"; RELAY="127.0.0.1:$RELAY_PORT"
ER_ADMIN_TOTP_OFF=1 ER_RATELIMIT_OFF=1 /tmp/er905-stack -addr "127.0.0.1:$GW_PORT" > "$OUT/stack.log" 2>&1 &
STACK_PID=$!
/tmp/er905-relay -addr "$RELAY" > "$OUT/relay.log" 2>&1 &
RELAY_PID=$!
wait_for "$GW/healthz" 40 || fail 'gateway not ready'

stop_host() {
  if [ -n "$HOST_PID" ]; then kill "$HOST_PID" 2>/dev/null; wait "$HOST_PID" 2>/dev/null; HOST_PID=""; fi
}
TAG="A"
start_host() {
  rm -f "$OUT/hello.txt"
  env HOME="$TMP_HOME" "$@" "$GW" "$RELAY" 13700009101 123456 "$GW" "$OUT/hello.txt" 30 0 > "$OUT/host-$TAG.log" 2>&1 &
  HOST_PID=$!
  for _ in $(seq 1 60); do [ -s "$OUT/hello.txt" ] && break; sleep 0.2; done
  DEVICE_CODE=$(sed -n 's/^device_code=//p' "$OUT/hello.txt")
  [ -n "$DEVICE_CODE" ] || fail 'host did not announce'
  PASSWORD=$(cat "$TMP_HOME/.easyremote/host_password" 2>/dev/null)
  [ -n "$PASSWORD" ] || fail 'host password missing'
}
run_probe() {
  ( cd "$ROOT" && ER_CORE_LIB="$ROOT/core/target/debug/liber_ffi.dylib" \
    dart run client/tool/lifecycle_probe.dart "$GW" "$RELAY" "$DEVICE_CODE" "$PASSWORD" "$@" ) \
    > "$OUT/probe-$TAG.log" 2>&1
}

HOST_BIN="$ROOT/core/target/debug/examples/mac_host"
TAG="A"

# ---------- A：主控断开 ----------
start_host "$HOST_BIN"
echo "E2E[A] device=$DEVICE_CODE"
run_probe --close || fail 'A: probe failed'
grep -q 'PROBE_CLOSED state=closed' "$OUT/probe-$TAG.log" || fail 'A: close not confirmed'
A0=$(date +%s); A_OK=0
for _ in $(seq 1 10); do grep -q 'session_closed' "$OUT/host-$TAG.log" && { A_OK=1; break; }; sleep 0.2; done
A_DT=$(( $(date +%s) - A0 ))
[ "$A_OK" = 1 ] || fail 'A: host did not converge within 2s'
echo "E2E[A]_PASS converged=${A_DT}s"
stop_host

# ---------- B：被控端断开 ----------
TAG="B"
ER_CLOSE_AFTER_SECS=1 start_host "$HOST_BIN"
echo "E2E[B] device=$DEVICE_CODE"
run_probe --watch-peer || fail 'B: controller did not observe peer close'
grep -q 'PROBE_PEER_CLOSED' "$OUT/probe-$TAG.log" || fail 'B: missing PROBE_PEER_CLOSED'
grep -q 'local_close_ok' "$OUT/host-$TAG.log" || fail 'B: host did not close locally'
echo "E2E[B]_PASS $(grep -o 'PROBE_PEER_CLOSED.*' "$OUT/probe-$TAG.log" | head -1)"
stop_host

# ---------- C：拒绝（真实双进程）----------
TAG="C"
ER_REJECT_ONCE=1 ER_SKIP_MEDIA_PROBE=1 start_host "$HOST_BIN"
echo "E2E[C] device=$DEVICE_CODE"
run_probe --wait-rejected || fail 'C: controller did not observe rejection'
grep -q 'PROBE_REJECTED reason=user_rejected' "$OUT/probe-$TAG.log" || fail 'C: missing rejection reason'
! grep -q 'PROBE_ATTACHED' "$OUT/probe-$TAG.log" || fail 'C: controller must not attach before authorized'
grep -q 'rejected_once' "$OUT/host-$TAG.log" || fail 'C: host did not reject'
echo 'E2E[C]_PASS rejected'
stop_host

echo 'E2E_ALL_PASS'
