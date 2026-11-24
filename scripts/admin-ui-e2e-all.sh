#!/usr/bin/env bash
# 管理端 E2E 一键回归（ADM-UI-10c）。
#
# 为什么需要它：12 个 E2E 脚本各自启动 stack、各自占用端口，逐个手敲容易漏跑。
# 这里串成一条命令：顺序执行（端口不冲突）→ 每个脚本日志落盘 → 汇总 PASS/FAIL。
# 注意：单次全量约 8-12 分钟，因此**不进 gate.sh**（gate 保持 20 步快速闸门）；
# 发版前或大改 UI 后手动跑这条。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${OUT_DIR:-$ROOT/docs/impl/evidence/ADM-UI-10c/logs}"
mkdir -p "$OUT"

SCRIPTS=(
  admin-ui-e2e-routes.py
  admin-ui-e2e-login.py
  admin-ui-e2e-dashboard.py
  admin-ui-e2e-users.py
  admin-ui-e2e-devices.py
  admin-ui-e2e-sessions.py
  admin-ui-e2e-config-audit.py
  admin-ui-e2e-admins.py
  admin-ui-e2e-risk.py
  admin-ui-e2e-versions.py
  admin-ui-e2e-dialogs.py
  admin-ui-e2e-lifecycle.py
  admin-ui-responsive-a11y.py
)

PASS=0
FAIL=0
FAILED=()
START=$(date +%s)

for s in "${SCRIPTS[@]}"; do
  name="${s%.py}"
  log="$OUT/$name.log"
  echo "==> $s"
  if ( cd "$ROOT" && python3 "scripts/$s" ) >"$log" 2>&1; then
    tail -1 "$log" | sed 's/^/    /'
    PASS=$((PASS+1))
  else
    echo "    FAILED（详见 $log）"
    tail -5 "$log" | sed 's/^/    /'
    FAIL=$((FAIL+1))
    FAILED+=("$s")
  fi
  # 脚本自己会收尾 stack，这里再等一拍，避免端口 TIME_WAIT 撞上下一个脚本。
  sleep 1
done

# 视觉基线：与最近一次 manifest 比对（缺 manifest 时只采集）。
# 视觉基线：跟最新一次"有意的视觉变更"对齐；变更 UI 后要同步这里。
MANIFEST="$ROOT/docs/impl/evidence/ADM-UI-12/screens/manifest.json"
echo "==> admin-ui-baseline.py"
BASE_LOG="$OUT/admin-ui-baseline.log"
if [ -f "$MANIFEST" ]; then
  if ( cd "$ROOT" && python3 scripts/admin-ui-baseline.py --out "$OUT/screens" --compare-to "$MANIFEST" ) >"$BASE_LOG" 2>&1; then
    tail -1 "$BASE_LOG" | sed 's/^/    /'
    PASS=$((PASS+1))
  else
    echo "    FAILED（详见 $BASE_LOG；先看是否属于预期视觉变更）"
    tail -8 "$BASE_LOG" | sed 's/^/    /'
    FAIL=$((FAIL+1))
    FAILED+=("admin-ui-baseline.py")
  fi
else
  echo "    SKIP（缺少基线 manifest：$MANIFEST）"
fi

ELAPSED=$(( $(date +%s) - START ))
echo ""
echo "== 汇总 =="
echo "  PASS: $PASS"
echo "  FAIL: $FAIL"
echo "  耗时: ${ELAPSED}s"
echo "  日志: $OUT"
if [ "$FAIL" -gt 0 ]; then
  echo "  E2E SUITE: FAIL（${FAILED[*]}）"
  exit 1
fi
echo "  E2E SUITE: PASS"
