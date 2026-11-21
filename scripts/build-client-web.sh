#!/usr/bin/env bash
# 构建客户端 Web 产物（同源模式）。
#
#   bash scripts/build-client-web.sh            # release
#   bash scripts/build-client-web.sh --debug    # debug（带 sourcemap）
#
# 铁律：Web 产物必须把 ER_GATEWAY 置空，请求走相对路径 /v1/... 命中 8091 同源代理；
# 一旦把 http://127.0.0.1:8090 烧进 bundle，浏览器就是 8091→8090 跨源，预检必被拦。
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MODE="--release"
[ "${1:-}" = "--debug" ] && MODE="--debug"
FLUTTER="${FLUTTER:-flutter}"
cd "$ROOT/client"
"$FLUTTER" build web "$MODE" --no-web-resources-cdn --dart-define=ER_GATEWAY=
BUNDLE="$ROOT/client/build/web/main.dart.js"
if grep -qE '127\.0\.0\.1:(8080|8090)' "$BUNDLE"; then
  echo "[build-client-web] 失败：产物里出现绝对网关地址，会在浏览器里触发跨源（CORS）" >&2
  exit 1
fi
echo "[build-client-web] OK：Web 产物走同源相对路径（/v1 由 8091 代理转发）"
