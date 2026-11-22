#!/usr/bin/env bash
# EasyRemote 严格闸门：本地与 CI 共用。
#
# 用法:
#   bash scripts/gate.sh                   严格闸门（任一步红即失败）
#   bash scripts/gate.sh --report          报告模式（只打印，不判红；不能作为完成证明）
#   bash scripts/gate.sh --update-baseline 更新禁止模式基线（需负责人批准）
#   bash scripts/gate.sh --only=go-test,rust-clippy
#   bash scripts/gate.sh --list            列出步骤
#
# 步骤定义在 scripts/gate.mjs；改命令只改那里，CI 对照测试会同步校验。
set -uo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
exec node scripts/gate.mjs "$@"
