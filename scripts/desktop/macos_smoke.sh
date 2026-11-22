#!/usr/bin/env bash
# ER-900 macOS 桌面关键路径冒烟：构建核心、跑平台采集/注入测试与跨进程 E2E。
# 不包含 flutter build macos（本机可能无 Xcode）；该步骤在矩阵文档中单独标记。
set -euo pipefail
cd "$(dirname "$0")/../.."

echo "== host =="
uname -a
sw_vers 2>/dev/null || true

echo "== rust platform tests =="
export PATH="$(dirname "$(rustup which cargo)"):$PATH"
(cd core && cargo test -p er-capture -p er-input)

echo "== ffi cdylib =="
(cd core && cargo build -p er-ffi)

echo "== flutter cross-process e2e (no skip) =="
(cd client && flutter test --concurrency=1 test/host_link_test.dart test/session_e2e_test.dart test/lossy_e2e_test.dart)

echo "== done =="
