#!/usr/bin/env bash
# EasyRemote 闸门：本地与 CI 共用。任何一步红，任务就不算完成。
# 用法:
#   bash scripts/gate.sh                   严格闸门（默认：信封/路由/契约不一致即红）
#   bash scripts/gate.sh --report          报告模式（只打印待办，不判红）
#   bash scripts/gate.sh --update-baseline 更新禁止模式基线（需在 PR 说明）
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if [ $# -gt 0 ]; then MODE="$1"; else MODE=""; fi
BASELINE="scripts/gate-baseline.txt"
fail=0
say() { echo; echo "== $1 =="; }

say "1/9 gofmt"
out=$(gofmt -l server 2>/dev/null || true)
if [ -n "$out" ]; then echo "$out"; fail=1; fi

say "2/9 go vet + go test"
if ! (cd server && go vet ./... && go test ./...); then fail=1; fi

say "3/9 web-admin tsc"
if [ -d web-admin/node_modules ]; then
  if ! (cd web-admin && npx tsc --noEmit); then fail=1; fi
else
  echo "跳过：web-admin/node_modules 不存在（先装依赖）"
fi

say "4/9 契约与错误码生成物"
node scripts/gen-errors.mjs --check || fail=1
node scripts/gen-contract.mjs --check || fail=1

say "5/9 路由清单 vs 契约"
if [ "$MODE" = "--report" ]; then
  node scripts/check-routes.mjs || true
else
  node scripts/check-routes.mjs --strict || fail=1
fi

say "6/9 禁止模式"
current="$(mktemp)"
trap 'rm -f "$current"' EXIT
{
  grep -rnF --include=*.go -e 'json.NewEncoder' -e 'http.Error(' -e 'w.Write([]byte' server/internal server/cmd 2>/dev/null | grep -v '_test.go' | grep -v 'server/internal/httpx/httpx.go' || true
  grep -rnF --include=*.go -e 'map[string]any' server/internal/adminhttp server/internal/gateway 2>/dev/null | grep -v '_test.go' || true
} | cut -d: -f1 | sort | uniq -c | awk '{print $2" "$1}' | sort > "$current"

if [ "$MODE" = "--update-baseline" ]; then
  cp "$current" "$BASELINE"
  echo "已更新基线：$BASELINE（$(wc -l < "$BASELINE" | tr -d ' ') 个文件）"
elif [ ! -f "$BASELINE" ]; then
  echo "缺少基线文件，先跑 bash scripts/gate.sh --update-baseline"
  fail=1
else
  offenders=$(awk 'NR==FNR{base[$1]=$2; next} { if ($2 > (base[$1]+0)) print "  "$1" 新增 "($2-base[$1])" 处（基线 "base[$1]+0"）" }' "$BASELINE" "$current")
  if [ -n "$offenders" ]; then
    echo "新增违规（迁移完成前不许再扩散）："
    echo "$offenders"
    fail=1
  else
    echo "无新增违规（存量 $(wc -l < "$BASELINE" | tr -d ' ') 个文件待迁移）"
  fi
fi

say "7/9 版本一致性"
node scripts/check-version.mjs || fail=1

say "8/9 响应信封契约"
if [ "$MODE" = "--report" ]; then
  (cd server && go test ./internal/adminhttp/ -run TestAdminEnvelopeContract -v 2>&1) | grep -E '违规|报告模式|全部' || true
else
  if ! (cd server && ER_ENVELOPE_STRICT=1 go test ./internal/adminhttp/ -run TestAdminEnvelopeContract); then fail=1; fi
fi

say "结果"
if [ "$fail" -eq 0 ]; then echo "GATE: PASS"; else echo "GATE: FAIL"; fi
exit "$fail"
