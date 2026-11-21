#!/usr/bin/env bash
# step3 · 跨机会话（Mac 主控 + Windows 被控）的 Mac 侧编排
#
# 为什么要两段：主控是 Mac 上的 Rust 二进制（真采集 + NVENC 只在被控侧），
# Windows 没有 Mac 的 SSH 密钥，没法自己把主控拉起来。硬凑"一键"只会造出假数据。
#
# 用法：
#   bash scripts/mac/step3_xmachine.sh [主控手机号]
#
# 前置：
#   - ssh 别名 win 可用（frp → 被控机 22）
#   - 被控机已解锁（脚本会先查 LogonUI，锁着直接退出，不产生 PASS/FAIL）
#   - 已构建：/tmp/erbin/{stack,relay,udptun}、core/target/debug/examples/xmachine_controller
#
# 退出码：0=PASS 1=FAIL 3=SKIP（锁屏/前置不满足）

set -u -o pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
EVID="$REPO/docs/impl/evidence/windows-2026-11-20-gpu"
BIN=/tmp/erbin
MAC_PHONE="${1:-13700007102}"
mkdir -p "$EVID"

say() { printf '%s\n' "$*"; }
skip() { say "SKIP: $*"; exit 3; }
fail() { say "FAIL: $*"; exit 1; }

# ---------- 0. 本地前置 ----------
for f in "$BIN/stack" "$BIN/relay" "$BIN/udptun" "$REPO/core/target/debug/examples/xmachine_controller"; do
  [ -x "$f" ] || skip "缺少二进制 $f（先构建）"
done
command -v ssh >/dev/null || skip "没有 ssh"
ssh -o BatchMode=yes -o ConnectTimeout=10 win "echo ok" >/dev/null 2>&1 || skip "ssh win 不可达"

# 注意：不要用 LogonUI.exe 判断锁屏 —— 它在解锁状态下也可能常驻，会误判成 SKIP。
# 权威判定是 session 1 里的 OpenInputDesktop，由 Windows 侧 payload 自己报。

# ---------- 1. Mac 侧真实服务 ----------
say "== 启动 stack / relay / udptun"
pkill -f "$BIN/stack"   2>/dev/null
pkill -f "$BIN/relay"   2>/dev/null
pkill -f "$BIN/udptun"  2>/dev/null
sleep 1
("$BIN/stack"  -addr 127.0.0.1:18090 > "$BIN/stack.log"  2>&1 &)
("$BIN/relay"  -addr 127.0.0.1:19000 > "$BIN/relay.log"  2>&1 &)
("$BIN/udptun" -mode server -listen 127.0.0.1:19001 -target 127.0.0.1:19000 > "$BIN/udptun.log" 2>&1 &)
sleep 2

# ---------- 2. 反向隧道 ----------
say "== 建立 ssh -R 隧道（18090 / 19001）"
TUNNEL_PIDFILE=/tmp/step3_tunnel.pid
rm -f "$TUNNEL_PIDFILE"
ssh -N -o BatchMode=yes -o ExitOnForwardFailure=yes -o ServerAliveInterval=20 \
    -R 18090:127.0.0.1:18090 -R 19001:127.0.0.1:19001 win &
echo $! > "$TUNNEL_PIDFILE"
sleep 2
cleanup() {
  [ -f "$TUNNEL_PIDFILE" ] && kill "$(cat "$TUNNEL_PIDFILE")" 2>/dev/null
  pkill -f "$BIN/stack" 2>/dev/null; pkill -f "$BIN/relay" 2>/dev/null; pkill -f "$BIN/udptun" 2>/dev/null
}
trap cleanup EXIT

# ---------- 3. Windows 侧被控端（后台，它会等旗标） ----------
WIN_PHONE="1370$(date +%s | tail -c 7)"
say "== 启动 Windows 被控端（phone=$WIN_PHONE）"
ssh -o BatchMode=yes win "cmd /c C:\\EasyRemote\\run_xmachine_gpu.cmd $WIN_PHONE $MAC_PHONE" \
    > "$EVID/step3-xmachine-win-wrapper.log" 2>&1 &
WRAP_PID=$!

# ---------- 4. 等 hello ----------
say "== 等被控端写出 device_code/captcha"
HELLO=/tmp/step3_win_hello.txt
rm -f "$HELLO"
for _ in $(seq 1 60); do
  if scp -o BatchMode=yes win:C:/EasyRemote/win_hello.txt "$HELLO" >/dev/null 2>&1; then
    grep -q 'captcha=' "$HELLO" && break
  fi
  sleep 1
done
[ -s "$HELLO" ] || fail "60s 内没拿到 hello（看 $EVID/step3-xmachine-win-wrapper.log）"
cp "$HELLO" "$EVID/step3-hello.txt"
say "   hello: $(tr '\n' ' ' < "$HELLO")"

# ---------- 5. Mac 主控 ----------
say "== 跑 Mac 主控"
"$REPO/core/target/debug/examples/xmachine_controller" \
  http://127.0.0.1:18090 127.0.0.1:19000 "$MAC_PHONE" 123456 "$HELLO" 5 8 400 300 \
  2>&1 | tee "$EVID/step3-xmachine-controller-macos.log"
CTRL_RC=${PIPESTATUS[0]}

# ---------- 6. 通知 Windows 侧收尾 ----------
say "== 写回旗标，等 Windows 侧断言"
echo "done $(date -u +%FT%TZ)" > /tmp/gpu_session.done
scp -o BatchMode=yes /tmp/gpu_session.done win:C:/EasyRemote/gpu_session.done >/dev/null 2>&1 || true
wait $WRAP_PID || true

# ---------- 7. 收日志 + 断言 ----------
scp -o BatchMode=yes win:C:/EasyRemote/step3logs/xmachine_gpu.log "$EVID/step3-xmachine-target-windows.log" >/dev/null 2>&1 || true
cat "$EVID/step3-xmachine-win-wrapper.log" >> "$EVID/step3-xmachine-target-windows.log" 2>/dev/null || true

TLOG="$EVID/step3-xmachine-target-windows.log"
[ -s "$TLOG" ] || fail "没拿到被控端日志"

say ""
say "== 断言（被控端原始日志）"
grep -E 'CODEC: backend|frames_received=|frames_sent=|cursor_after=|moved_to_target=|backend_final' "$TLOG" | sed 's/^/   /'

ok=1
grep -q 'CODEC: backend=gpu-zero-copy' "$TLOG"        || { say "   ✗ 缺少 backend=gpu-zero-copy"; ok=0; }
grep -q 'CODEC: backend_final=gpu-zero-copy' "$TLOG"  || { say "   ✗ backend_final 不是 gpu-zero-copy"; ok=0; }
grep -q 'moved_to_target=true' "$TLOG"                || { say "   ✗ 光标没有移动到目标"; ok=0; }
if [ $CTRL_RC -ne 0 ]; then say "   ✗ 主控退出码 $CTRL_RC"; ok=0; fi

if [ $ok -eq 1 ]; then
  say "STEP3_RESULT: PASS（零拷贝真机启用 + 跨机全链路 + 注入到位）"
  exit 0
else
  say "STEP3_RESULT: FAIL（详见 $TLOG）"
  exit 1
fi
