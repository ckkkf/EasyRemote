#!/usr/bin/env bash
# 启动 macOS 被控端，供 Windows 主控端远控。
#
# 做三件事：
#   1. 起一体化服务（gateway + 信令 + 中继）和 UDP 隧道服务端
#   2. 开 ssh -R 反向隧道，让 Windows 上的 127.0.0.1:18090/19001 通到这台 Mac
#   3. 起 mac_host：注入主控输入 + 持续共享本机屏幕，打印 9 位设备码 + 6 位验证码
#
# 用法：
#   bash scripts/mac/start_host.sh [时长秒，默认 3600]
#
# 退出码：0=正常结束 3=前置不满足
set -u -o pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BIN=/tmp/erbin
MAC_HOST="$REPO/core/target/debug/examples/mac_host"
SECS="${1:-3600}"
TARGET_PHONE="${2:-13700009101}"
HELLO=/tmp/er_host_hello.txt

say() { printf "%s\n" "$*"; }

for f in "$BIN/stack" "$BIN/relay" "$BIN/udptun" "$MAC_HOST"; do
  [ -x "$f" ] || { say "缺少二进制 $f（先构建）"; exit 3; }
done
ssh -o BatchMode=yes -o ConnectTimeout=10 win "echo ok" >/dev/null 2>&1 || { say "ssh win 不可达"; exit 3; }

pkill -f "$BIN/stack" 2>/dev/null || true
pkill -f "$BIN/relay" 2>/dev/null || true
pkill -f "$BIN/udptun" 2>/dev/null || true
sleep 1
"$BIN/stack"  -addr 127.0.0.1:18090 > /tmp/er_host_stack.log 2>&1 &
"$BIN/relay"  -addr 127.0.0.1:19000 > /tmp/er_host_relay.log 2>&1 &
"$BIN/udptun" -mode server -listen 127.0.0.1:19001 -target 127.0.0.1:19000 > /tmp/er_host_udptun.log 2>&1 &
sleep 2

ssh -N -o BatchMode=yes -o ExitOnForwardFailure=yes -o ServerAliveInterval=20 \
    -R 18090:127.0.0.1:18090 -R 19001:127.0.0.1:19001 win &
TUNNEL=$!
sleep 2

cleanup() {
  kill "$TUNNEL" 2>/dev/null || true
  pkill -f "$BIN/stack" 2>/dev/null || true
  pkill -f "$BIN/relay" 2>/dev/null || true
  pkill -f "$BIN/udptun" 2>/dev/null || true
}
trap cleanup EXIT

rm -f "$HELLO"
"$MAC_HOST" http://127.0.0.1:18090 127.0.0.1:19000 "$TARGET_PHONE" 123456 \
    http://127.0.0.1:18090 "$HELLO" 30 "$SECS" &
HOST=$!

for _ in $(seq 1 60); do
  if [ -s "$HELLO" ] && grep -q captcha= "$HELLO"; then
    say ""
    say "=================== 被控端已就绪 ==================="
    say "  设备码：$(sed -n "s/^device_code=//p" "$HELLO")"
    say "  验证码：$(sed -n "s/^captcha=//p" "$HELLO")"
    say "  在 Windows 的 EasyRemote 里填这两个数字即可连接"
    say "  （本机屏幕会被共享，鼠标键盘由对端控制；Ctrl-C 结束）"
    say "===================================================="
    break
  fi
  sleep 1
done

wait "$HOST"
