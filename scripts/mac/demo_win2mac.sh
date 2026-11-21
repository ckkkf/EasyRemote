#!/usr/bin/env bash
# step4-demo · Windows 远控 macOS 的**可视化演示**。
#
# 演示内容：
#   1) Windows 主控画一个圆（60 个鼠标事件）→ macOS 被控端实时注入，
#      肉眼可见光标在 macOS 桌面上转圈——这就是「远控真的在动」。
#   2) 注入结束后 macOS 采集自己的真实屏幕并编码回传 30 帧，
#      Windows 端把 H.264 码流落盘，本脚本用 ffmpeg 转成 mp4 + 抽关键帧 PNG。
#
# 用法：
#   bash scripts/mac/demo_win2mac.sh [演示秒数，默认 2]
#
# 产出（docs/impl/evidence/win2mac-demo/）：
#   demo_mac.mp4         Windows 端录下的 macOS 桌面（H.264 转码）
#   demo_mac_*.png       关键帧截图
#   win-demo.log / mac-target.log / demo_mac.h264
set -u -o pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
EVID="$REPO/docs/impl/evidence/win2mac-demo"
BIN=/tmp/erbin
MAC_TARGET="$REPO/core/target/debug/examples/mac_target"
DEMO_SECS="${1:-2}"
CTRL_PHONE="${2:-1370$(date +%s | tail -c 7)}"
TARGET_PHONE="${3:-13700008101}"
mkdir -p "$EVID"

say() { printf "%s\n" "$*"; }
skip() { say "SKIP: $*"; exit 3; }
fail() { say "FAIL: $*"; exit 1; }

for f in "$BIN/stack" "$BIN/relay" "$BIN/udptun" "$MAC_TARGET"; do
  [ -x "$f" ] || skip "缺少二进制 $f（先构建）"
done
command -v ffmpeg >/dev/null || skip "没有 ffmpeg（无法转码）"
[ "$(launchctl managername 2>/dev/null || echo unknown)" = "Aqua" ] || fail "必须在 Aqua 登录会话里跑"
ssh -o BatchMode=yes -o ConnectTimeout=10 win "echo ok" >/dev/null 2>&1 || skip "ssh win 不可达"

pkill -f "$BIN/stack" 2>/dev/null || true
pkill -f "$BIN/relay" 2>/dev/null || true
pkill -f "$BIN/udptun" 2>/dev/null || true
sleep 1
"$BIN/stack"  -addr 127.0.0.1:18090 > "$BIN/stack.log"  2>&1 &
"$BIN/relay"  -addr 127.0.0.1:19000 > "$BIN/relay.log"  2>&1 &
"$BIN/udptun" -mode server -listen 127.0.0.1:19001 -target 127.0.0.1:19000 > "$BIN/udptun.log" 2>&1 &
sleep 2

TUNNEL_PIDFILE=/tmp/demo2_tunnel.pid
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

HELLO=/tmp/demo2_mac_hello.txt
rm -f "$HELLO"
say "== 启动 macOS 被控端（演示模式：先注入、后回传 30 帧）"
"$MAC_TARGET" http://127.0.0.1:18090 127.0.0.1:19000 "$TARGET_PHONE" 123456 \
    http://127.0.0.1:18090 5 30 "$HELLO" 60 40 1 \
    > "$EVID/mac-target.log" 2>&1 &
TARGET_PID=$!

for _ in $(seq 1 60); do
  [ -s "$HELLO" ] && grep -q "captcha=" "$HELLO" && break
  sleep 1
done
[ -s "$HELLO" ] || fail "被控端没有写出 hello"
say "   hello: $(tr "\n" " " < "$HELLO")"

say "== 起 Windows 主控端（画圆 ${DEMO_SECS}s + 录像）"
scp -o BatchMode=yes "$HELLO" win:C:/EasyRemote/mac_hello.txt >/dev/null 2>&1 || fail "scp hello 失败"
ssh -o BatchMode=yes win "C:\EasyRemote\run_win2mac_demo.cmd $CTRL_PHONE $DEMO_SECS" \
    > "$EVID/win-demo.log" 2>&1 || true

wait "$TARGET_PID" 2>/dev/null || true

scp -o BatchMode=yes win:C:/EasyRemote/demo_mac.h264 "$EVID/demo_mac.h264" >/dev/null 2>&1 || true
[ -s "$EVID/demo_mac.h264" ] || fail "没拿到 Windows 端录像（见 $EVID/win-demo.log）"
say "   收到码流 $(wc -c < "$EVID/demo_mac.h264" | tr -d " ") 字节"

say "== ffmpeg 转码 / 抽帧"
ffmpeg -y -loglevel error -f h264 -i "$EVID/demo_mac.h264" -vf "fps=25,scale=1280:-2" \
  -c:v libx264 -pix_fmt yuv420p -movflags +faststart "$EVID/demo_mac.mp4" 2>/dev/null || true
# ffmpeg 7+ 用 -fps_mode 取代了 -vsync（9.x 已移除 -vsync，写错会直接报错）
ffmpeg -y -loglevel error -f h264 -i "$EVID/demo_mac.h264" -vf "select=not(mod(n\\,6))" \
  -fps_mode vfr "$EVID/demo_mac_%02d.png" 2>/dev/null || true

# 把被控端实际注入的坐标画到截图上：一眼看出 Windows 发来的轨迹（圆）
python3 - "$EVID/mac-target.log" "$EVID/demo_mac_01.png" "$EVID/demo_mac_trajectory.png" <<'PY' 2>/dev/null || true
import re, sys
log = open(sys.argv[1], encoding="utf-8", errors="ignore").read()
m = re.search(r"injected_moves=\[(.*?)\]", log, re.S)
if m:
    pts = [(int(x), int(y)) for x, y in re.findall(r"\((\d+), (\d+)\)", m.group(1))]
    from PIL import Image, ImageDraw
    img = Image.open(sys.argv[2]).convert("RGB")
    d = ImageDraw.Draw(img)
    d.line(pts, fill=(255, 60, 60), width=5)
    for i, (x, y) in enumerate(pts):
        if i % 6 == 0:
            d.ellipse([x - 7, y - 7, x + 7, y + 7], outline=(255, 200, 0), width=3)
    img.save(sys.argv[3])
    print(f"   trajectory_points={len(pts)} -> {sys.argv[3].split('/')[-1]}")
PY

say ""
say "== 结果"
grep -E "MAC: (permission_screen|frames_sent|injected_moves|moved_to_target|VERDICT)" "$EVID/mac-target.log" | sed "s/^/   /" || true
grep -E "WIN: (demo_moves_sent|recorded_frames|frames_from_target|VERDICT)" "$EVID/win-demo.log" | sed "s/^/   /" || true
say ""
ls -la "$EVID" | sed "s/^/   /"

if grep -q "MAC: VERDICT PASS" "$EVID/mac-target.log" && grep -q "WIN: VERDICT PASS" "$EVID/win-demo.log"; then
  say "DEMO_RESULT: PASS（录像：$EVID/demo_mac.mp4）"
  exit 0
fi
fail "演示未完全通过（见 $EVID）"
