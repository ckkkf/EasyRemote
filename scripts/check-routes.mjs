#!/usr/bin/env node
// 校验「契约端点（method/path/auth）↔ 代码声明（RouteSpec）↔ 已注册路由」三方一致。
// 用法: node scripts/check-routes.mjs [--strict]
// 默认报告模式：列出差异但不失败；--strict 差异即失败（gate 使用）。
import { readFileSync } from 'node:fs';

const ROUTERS = ['server/internal/adminhttp/handler.go', 'server/internal/gateway/handler.go'];
const SPEC_FILES = ['server/internal/adminhttp/route_specs.go', 'server/internal/gateway/route_specs.go'];
const CONTRACT = 'docs/spec/contract.yaml';

/** 解析 contract.yaml 的 endpoints（ID/Method/Path/Auth）。 */
export function parseContract(text) {
  const out = [];
  let cur = null;
  const flush = () => {
    if (cur && cur.method && cur.path && cur.auth) out.push(cur);
    cur = null;
  };
  for (const raw of text.split('\n')) {
    const t = raw.trim();
    const m = t.match(/^([A-Z]+-\d+):$/);
    if (m) {
      flush();
      cur = { id: m[1] };
      continue;
    }
    if (!cur) continue;
    if (t.startsWith('method:')) cur.method = t.slice(7).trim();
    else if (t.startsWith('path:')) cur.path = t.slice(5).trim();
    else if (t.startsWith('auth:')) cur.auth = t.slice(5).trim();
    else if (t.startsWith('response:')) cur.response = t.slice(9).trim();
  }
  flush();
  return out;
}

/** 解析 *.go 里注册的 path（当前仍是无 method 的 HandleFunc("/path", ...)）。 */
export function parseCodePaths(text) {
  const paths = [];
  const re = /HandleFunc\("([^"]+)"/g;
  let m;
  while ((m = re.exec(text))) paths.push(m[1]);
  return paths;
}

/** 解析 RouteSpec 声明块。 */
export function parseSpecs(text) {
  const out = [];
  const re = /\{ID:\s*"([^"]+)",\s*Method:\s*"([^"]+)",\s*Path:\s*"([^"]+)",\s*Auth:\s*"([^"]+)"\}/g;
  let m;
  while ((m = re.exec(text))) out.push({ id: m[1], method: m[2], path: m[3], auth: m[4] });
  return out;
}

/** 对比三方，返回 issue 列表（空数组 = 一致）。
 *
 * 同一 path 可以有多条契约端点（例如 /admin/v1/config 的 GET 与 PUT），
 * 因此端点与声明都按「path + method」对齐；代码注册仍按 path。
 */
export function compareRoutes(contract, codePaths, specs) {
  const issues = [];
  const code = new Set(codePaths);
  const contractKeys = new Set();
  const specKeys = new Set();

  for (const ep of contract) {
    const key = ep.path + ' ' + ep.method;
    if (contractKeys.has(key)) issues.push({ kind: 'contract-duplicate', id: ep.id, path: key });
    contractKeys.add(key);
  }
  const specSeen = new Map();
  for (const s of specs) {
    const key = s.path + ' ' + s.method;
    const prev = specSeen.get(key);
    if (prev) {
      // /healthz 在 gateway 与 admin 各注册一次；同 ID/同语义的重复声明允许。
      if (prev.id !== s.id || prev.auth !== s.auth) {
        issues.push({ kind: 'spec-duplicate', id: s.id, path: key });
      }
    } else {
      specSeen.set(key, s);
    }
    specKeys.add(key);
  }

  for (const ep of contract) {
    if (!code.has(ep.path)) issues.push({ kind: 'ghost', id: ep.id, path: ep.path, detail: '契约有、代码未注册' });
    const spec = specs.find((s) => s.path === ep.path && s.method === ep.method);
    if (!spec) {
      issues.push({ kind: 'missing-spec', id: ep.id, path: ep.path + ' ' + ep.method, detail: '代码没有对应 method 的声明' });
      continue;
    }
    if (spec.id !== ep.id) issues.push({ kind: 'id-drift', path: ep.path + ' ' + ep.method, detail: ep.id + ' ≠ ' + spec.id });
    if (spec.auth !== ep.auth) {
      issues.push({ kind: 'auth-drift', id: ep.id, path: ep.path + ' ' + ep.method, detail: '契约 ' + ep.auth + ' ≠ 代码 ' + spec.auth });
    }
  }

  for (const s of specs) {
    if (!contractKeys.has(s.path + ' ' + s.method)) {
      const samePath = contract.filter((e) => e.path === s.path);
      if (samePath.length > 0) {
        issues.push({ kind: 'method-drift', id: s.id, path: s.path, detail: '代码 ' + s.method + ' 不在契约（' + samePath.map((e) => e.method).join('/') + '）' });
      } else {
        issues.push({ kind: 'spec-undeclared', id: s.id, path: s.path, detail: '代码声明未进契约' });
      }
    }
    if (!code.has(s.path)) issues.push({ kind: 'spec-unregistered', id: s.id, path: s.path, detail: '声明了但没注册 handler' });
  }
  for (const p of code) {
    if (!contract.some((e) => e.path === p)) issues.push({ kind: 'undeclared', path: p, detail: '代码注册未进契约' });
  }
  return issues;
}

function printIssues(issues) {
  const order = ['method-drift', 'auth-drift', 'id-drift', 'missing-spec', 'ghost', 'undeclared', 'spec-undeclared', 'spec-unregistered', 'contract-duplicate', 'spec-duplicate'];
  issues.sort((a, b) => order.indexOf(a.kind) - order.indexOf(b.kind) || String(a.path).localeCompare(String(b.path)));
  for (const i of issues) {
    const label = i.kind.padEnd(18);
    console.log('  ' + label + (i.id ? i.id + ' ' : '') + i.path + (i.detail ? '：' + i.detail : ''));
  }
}

function main() {
  const strict = process.argv.includes('--strict');
  const contract = parseContract(readFileSync(CONTRACT, 'utf8'));
  const specs = SPEC_FILES.flatMap((f) => parseSpecs(readFileSync(f, 'utf8')));
  const codePaths = ROUTERS.flatMap((f) => parseCodePaths(readFileSync(f, 'utf8')));
  const issues = compareRoutes(contract, codePaths, specs);

  if (issues.length === 0) {
    console.log('✓ 路由 method/path/auth 与契约一致（' + contract.length + ' 条端点）');
    return 0;
  }
  console.log('路由与契约不一致（' + issues.length + ' 项）：');
  printIssues(issues);
  console.log(strict ? '严格模式：判红' : '报告模式：不判红');
  return strict ? 1 : 0;
}

if (process.argv[1] && process.argv[1].endsWith('check-routes.mjs')) {
  process.exit(main());
}
