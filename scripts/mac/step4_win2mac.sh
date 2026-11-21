#!/usr/bin/env bash
# step4 · 跨机会话（Windows 主控 + macOS 被控）的 Mac 侧编排。
#
# 与 step3 镜像：step3 是 Mac 主控 + Windows 被控；step4 是 Windows 主控 + macOS 被控。
# macOS 被控端必须跑在**有 WindowServer 的登录会话**里（屏幕录制 + CGEventPost 都需要），
# 所以由本脚本从桌面会话直接拉起；Windows 主控端只发输入/收画面、不注入本机，
# 因此可以在 SSH 会话里跑，不需要 schtasks /IT。
#
# 用法：
#   bash scripts/mac/step4_win2mac.sh [主控手机号] [被控手机号]
#
# 前置：
#   - ssh 别名 win 可用（frp → 被控机 22）
#   - /tmp/erbin/{stack,relay,udptun} 与 core/target/debug/examples/mac_target 已构建
#   - 本脚本在 Aqua 会话里执行（launchctl managername = Aqua）
#
# 退出码：0=PASS 1=FAIL 3=SKIP（前置不满足）

set -u -o pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
EVID="$REPO/docs/impl/evidence/win2mac-2026-11-20"
BIN=/tmp/erbin
MAC_TARGET="$REPO/core/target/debug/examples/mac_target"
CTRL_PHONE="${1:-13700008102}"
TARGET_PHONE="${2:-13700008101}"
mkdir -p "$EVID"

say() { printf "%s\n" "$*"; }
skip() { say "SKIP: $*"; exit 3; }
fail() { say "FAIL: $*"; exit 1; }

# ---------- 0. 前置 ----------
for f in "$BIN/stack" "$BIN/relay" "$BIN/udptun" "$MAC_TARGET"; do
  [ -x "$f" ] || skip "缺少二进制 $f（先构建）"
done
command -v ssh >/dev/null || skip "没有 ssh"

MANAGER="$(launchctl managername 2>/dev/null || echo unknown)"
say "== 会话类型：$MANAGER"
if [ "$MANAGER" != "Aqua" ]; then
  fail "必须在 Aqua（桌面登录）会话里跑：$MANAGER。SSH 后台会话没有 WindowServer，采集/注入都会失败。"
fi

ssh -o BatchMode=yes -o ConnectTimeout=10 win "echo ok" >/dev/null 2>&1 || skip "ssh win 不可达"

# ---------- 1. Mac 侧服务 ----------
say "== 启动 stack / relay / udptun"
pkill -f "$BIN/stack"  2>/dev/null || true
pkill -f "$BIN/relay"  2>/dev/null || true
pkill -f "$BIN/udptun" 2>/dev/null || true
sleep 1
"$BIN/stack"  -addr 127.0.0.1:18090 > "$BIN/stack.log"  2>&1 &
"$BIN/relay"  -addr 127.0.0.1:19000 > "$BIN/relay.log"  2>&1 &
"$BIN/udptun" -mode server -listen 127.0.0.1:19001 -target 127.0.0.1:19000 > "$BIN/udptun.log" 2>&1 &
sleep 2

# ---------- 2. 反向隧道：把 Mac 的 gateway/udptun 端口送到 Windows ----------
say "== 建立 ssh -R 隧道（18090 / 19001）"
TUNNEL_PIDFILE=/tmp/step4_tunnel.pid
rm -f "$TUNNEL_PIDFILE"
ssh -N -o BatchMode=yes -o ExitOnForwardFailure=yes -o ServerAliveInterval=20 \
    -R 18090:127.0.0.1:18090 -R 19001:127.0.0.1:19001 win &
echo $! > "$TUNNEL_PIDFILE"
sleep 2

TARGET_PID=""
cleanup() {
  [ -n "$TARGET_PID" ] && kill "$TARGET_PID" 2>/dev/null || true
  [ -f "$TUNNEL_PIDFILE" ] && kill "$(cat "$TUNNEL_PIDFILE")" 2>/dev/null || true
  pkill -f "$BIN/stack" 2>/dev/null || true
  pkill -f "$BIN/relay" 2>/dev/null || true
  pkill -f "$BIN/udptun" 2>/dev/null || true
}
trap cleanup EXIT

# ---------- 3. macOS 被控端（本机，桌面会话） ----------
HELLO=/tmp/step4_mac_hello.txt
rm -f "$HELLO"
say "== 启动 macOS 被控端（phone=$TARGET_PHONE）"
"$MAC_TARGET" http://127.0.0.1:18090 127.0.0.1:19000 "$TARGET_PHONE" 123456 \
    http://127.0.0.1:18090 5 5 "$HELLO" 3 \
    > "$EVID/mac-target.log" 2>&1 &
TARGET_PID=$!

say "== 等被控端写出 device_code/captcha"
for _ in $(seq 1 60); do
  if [ -s "$HELLO" ] && grep -q "captcha=" "$HELLO"; then break; fi
  sleep 1
done
[ -s "$HELLO" ] || fail "60s 内没拿到 hello（见 $EVID/mac-target.log）"
cp "$HELLO" "$EVID/step4-hello.txt"
say "   hello: $(tr "\n" " " < "$HELLO")"

# ---------- 4. 启动 Windows 主控端 ----------
say "== 起 Windows 主控端（phone=$CTRL_PHONE）"
scp -o BatchMode=yes "$HELLO" win:C:/EasyRemote/mac_hello.txt >/dev/null 2>&1 || fail "scp hello 到 Windows 失败"
ssh -o BatchMode=yes win "C:\\EasyRemote\\run_win2mac.cmd $CTRL_PHONE" \
    > "$EVID/win-controller.log" 2>&1 || true

# ---------- 5. 等被控端收尾 ----------
say "== 等 macOS 被控端收尾"
wait "$TARGET_PID" || true
TARGET_RC=$?

# ---------- 6. 收日志 + 断言 ----------
scp -o BatchMode=yes win:C:/EasyRemote/step4logs/win_controller.log "$EVID/win-controller-target.log" >/dev/null 2>&1 || true

TLOG="$EVID/mac-target.log"
WLOG="$EVID/win-controller.log"
[ -s "$TLOG" ] || fail "没有 macOS 被控端日志"

say ""
say "== 断言：macOS 被控端"
grep -E "MAC: (permission_screen|display|frames_received|frames_sent|moved_to_target|injected_mapped|RESULT|VERDICT)" "$TLOG" | sed "s/^/   /" || true
if [ -s "$WLOG" ]; then
  say "== 断言：Windows 主控端"
  grep -E "WIN: (frames_sent|input_events_sent|frames_from_target|RESULT|VERDICT)" "$WLOG" | sed "s/^/   /" || true
fi

ok=1
grep -q "MAC: permission_screen=ok" "$TLOG"   || { say "   x 屏幕录制权限未就绪"; ok=0; }
grep -q "moved_to_target=true" "$TLOG"        || { say "   x 光标没有移动到目标"; ok=0; }
grep -q "MAC: VERDICT PASS" "$TLOG"           || { say "   x 被控端 VERDICT 不是 PASS"; ok=0; }
grep -q "WIN: VERDICT PASS" "$WLOG" 2>/dev/null || { say "   x 主控端 VERDICT 不是 PASS"; ok=0; }
grep -q "frames_from_target=" "$WLOG" 2>/dev/null || { say "   x 主控端没有收到帧"; ok=0; }

if [ $ok -eq 1 ]; then
  say "STEP4_RESULT: PASS（Windows 发鼠标 → macOS 真注入 + macOS 真屏幕帧回到 Windows）"
  exit 0
fi
say "STEP4_RESULT: FAIL（详见 $EVID）"
exit 1
