#!/usr/bin/env bash
# Windows GUI 主控端联调：Mac 起服务 + 持续被控端，Windows 打开 er-winapp 自动连接。
#
# 用法：bash scripts/mac/demo_winapp.sh [秒数，默认 25]
set -u -o pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
EVID="$REPO/docs/impl/evidence/win2mac-app"
BIN=/tmp/erbin
MAC_HOST="$REPO/core/target/debug/examples/mac_host"
SECS="${1:-25}"
CTRL_PHONE="${2:-13700008202}"
TARGET_PHONE="${3:-13700009101}"
mkdir -p "$EVID"

say() { printf "%s\n" "$*"; }
skip() { say "SKIP: $*"; exit 3; }
fail() { say "FAIL: $*"; exit 1; }

for f in "$BIN/stack" "$BIN/relay" "$BIN/udptun" "$MAC_HOST"; do
  [ -x "$f" ] || skip "缺少二进制 $f"
done
ssh -o BatchMode=yes -o ConnectTimeout=10 win "echo ok" >/dev/null 2>&1 || skip "ssh win 不可达"

pkill -f "$BIN/stack" 2>/dev/null || true
pkill -f "$BIN/relay" 2>/dev/null || true
pkill -f "$BIN/udptun" 2>/dev/null || true
sleep 1
"$BIN/stack"  -addr 127.0.0.1:18090 > "$BIN/stack.log"  2>&1 &
"$BIN/relay"  -addr 127.0.0.1:19000 > "$BIN/relay.log"  2>&1 &
"$BIN/udptun" -mode server -listen 127.0.0.1:19001 -target 127.0.0.1:19000 > "$BIN/udptun.log" 2>&1 &
sleep 2

TUNNEL_PIDFILE=/tmp/app_tunnel.pid
rm -f "$TUNNEL_PIDFILE"
ssh -N -o BatchMode=yes -o ExitOnForwardFailure=yes -o ServerAliveInterval=20 \
    -R 18090:127.0.0.1:18090 -R 19001:127.0.0.1:19001 win &
echo $! > "$TUNNEL_PIDFILE"
sleep 2

HOST_PID=""
cleanup() {
  [ -n "$HOST_PID" ] && kill "$HOST_PID" 2>/dev/null || true
  [ -f "$TUNNEL_PIDFILE" ] && kill "$(cat "$TUNNEL_PIDFILE")" 2>/dev/null || true
  pkill -f "$BIN/stack" 2>/dev/null || true
  pkill -f "$BIN/relay" 2>/dev/null || true
  pkill -f "$BIN/udptun" 2>/dev/null || true
}
trap cleanup EXIT

HELLO=/tmp/app_mac_hello.txt
rm -f "$HELLO"
say "== 启动 macOS 被控端（持续流）"
"$MAC_HOST" http://127.0.0.1:18090 127.0.0.1:19000 "$TARGET_PHONE" 123456 \
    http://127.0.0.1:18090 "$HELLO" 30 "$SECS" \
    > "$EVID/mac-host.log" 2>&1 &
HOST_PID=$!

for _ in $(seq 1 60); do
  [ -s "$HELLO" ] && grep -q "captcha=" "$HELLO" && break
  sleep 1
done
[ -s "$HELLO" ] || fail "被控端没有写出 hello"
CODE=$(sed -n "s/^device_code=//p" "$HELLO")
CAPTCHA=$(sed -n "s/^captcha=//p" "$HELLO")
say "   device_code=$CODE captcha=$CAPTCHA"

say "== 写 er.conf 并启动 Windows GUI"
CONF=$(mktemp)
printf "gateway=http://127.0.0.1:18090\nrelay=127.0.0.1:19000\nphone=%s\ndevice_code=%s\ncaptcha=%s\nauto_connect=1\n" \
  "$CTRL_PHONE" "$CODE" "$CAPTCHA" > "$CONF"
cp "$CONF" "$EVID/er.conf"
ssh -o BatchMode=yes win "taskkill /im er-winapp.exe /f" >/dev/null 2>&1 || true
scp -o BatchMode=yes "$CONF" win:C:/EasyRemote/core/target/debug/er.conf >/dev/null 2>&1 || fail "scp er.conf 失败"
# schtasks /run 在「任务已在运行」时会返回非零——不算失败，下面用进程存在性判断
ssh -o BatchMode=yes win 'schtasks /run /tn ERWinAppLog' >/dev/null 2>&1 || true
sleep 3
ssh -o BatchMode=yes win 'tasklist /fi "imagename eq er-winapp.exe"' 2>/dev/null | grep -q er-winapp \
  || fail "er-winapp.exe 没起来（看 Windows 侧 C:\EasyRemote\app.log）"
sleep 15

say "== 截图（Windows 主控端窗口）"
ssh -o BatchMode=yes win "del /q C:\\EasyRemote\\win_screen.png" >/dev/null 2>&1 || true
ssh -o BatchMode=yes win 'schtasks /run /tn ERDiag' >/dev/null 2>&1 || true
sleep 6
scp -o BatchMode=yes win:C:/EasyRemote/win_screen.png "$EVID/winapp-screen.png" >/dev/null 2>&1 || true
[ -s "$EVID/winapp-screen.png" ] && say "   截图: $EVID/winapp-screen.png"

wait "$HOST_PID" 2>/dev/null || true

say ""
say "== 结果"
grep -E "MAC: (capture_ok|streaming|stats|HOST_DONE)" "$EVID/mac-host.log" | tail -8 | sed "s/^/   /" || true
if grep -q "HOST_DONE" "$EVID/mac-host.log" && [ -s "$EVID/winapp-screen.png" ]; then
  say "APP_DEMO: DONE（看 $EVID/winapp-screen.png）"
  exit 0
fi
fail "未完成（见 $EVID）"
