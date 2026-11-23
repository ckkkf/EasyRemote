#!/usr/bin/env bash
# 一键本地演示：一体化服务端(:18090) + 管理后台(:5199) + 客户端 Web 预览(:8091)
#
#   ./scripts/dev-demo.sh            # 全部拉起（前台，Ctrl-C 结束）
#
# 依赖：go、node/npm（web-admin/node_modules 已装）、flutter（仅首次构建客户端 Web 需要）
# 说明：客户端桌面版需要 Xcode（flutter run -d macos），没有 Xcode 时用 Web 预览页看 UI。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# 18090 是与桌面客户端构建产物、scripts/mac/* 一致的唯一开发端口，别再各写一份。
STACK_PORT="${STACK_PORT:-18090}"
ADMIN_PORT="${ADMIN_PORT:-5199}"
CLIENT_PORT="${CLIENT_PORT:-8091}"

cleanup() {
  echo
  echo "[dev-demo] 关闭子进程…"
  kill 0 2>/dev/null || true
}
trap cleanup EXIT INT TERM

echo "[dev-demo] 构建服务端 → /tmp/er-stack-new"
( cd "$ROOT/server" && go build -o /tmp/er-stack-new ./cmd/stack )

echo "[dev-demo] 构建客户端 Web（--dart-define=ER_GATEWAY= 走同源代理）"
bash "$ROOT/scripts/build-client-web.sh"

echo "[dev-demo] 启动一体化服务端 :${STACK_PORT}（内存存储 + 开发管理员 root/dev-pass，二次验证与限流已关闭）"
# ER_ADMIN_TOTP_OFF=1：本地联调关闭二次验证；生产环境配这个变量会直接启动失败。
# ER_RATELIMIT_OFF=1：本地联调关闭限流（登录 10 次/小时等阈值会把开发者自己锁死）；
# 生产环境配这个变量同样直接启动失败，见 config.ValidateDeployment。
ER_ADMIN_TOTP_OFF=1 ER_RATELIMIT_OFF=1 /tmp/er-stack-new -addr ":${STACK_PORT}" &

echo "[dev-demo] 启动管理后台 :${ADMIN_PORT}（代理 /admin → :${STACK_PORT}）"
( cd "$ROOT/web-admin" && npx vite --config vite.demo.config.ts --port "${ADMIN_PORT}" ) &

echo "[dev-demo] 启动客户端 Web 预览 :${CLIENT_PORT}（静态资源 + /v1、/admin 同源代理）"
node "$ROOT/scripts/serve-client-web.mjs" ":${STACK_PORT}" "${CLIENT_PORT}" &

sleep 2
cat <<EOF

  管理后台   http://127.0.0.1:${ADMIN_PORT}/     账号 root / dev-pass / 二次验证码留空（ER_ADMIN_TOTP_OFF=1）
  客户端预览 http://127.0.0.1:${CLIENT_PORT}/    手机号任意，短信验证码固定 123456

  开发短信固定码 123456；客户端原生采集/编解码/输入注入在 Web 预览里不可用（需桌面端）。
EOF

wait
