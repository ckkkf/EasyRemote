#!/usr/bin/env node
// 校验「代码里注册的路由」与「contract.yaml 声明的接口」是否一一对应。
// 用法: node scripts/check-routes.mjs [--strict]
// 默认报告模式：列出差异但不失败；--strict 时差异即失败（gate.sh --strict 使用）。
import { readFileSync } from 'node:fs';

const NL = String.fromCharCode(10);
const Q = String.fromCharCode(34);
const ROUTERS = ['server/internal/adminhttp/handler.go', 'server/internal/gateway/handler.go'];
const CONTRACT = 'docs/spec/contract.yaml';
const strict = process.argv.includes('--strict');

const contractPaths = new Set();
for (const line of readFileSync(CONTRACT, 'utf8').split(NL)) {
  const t = line.trim();
  if (t.startsWith('path:')) contractPaths.add(t.slice(5).trim());
}

const codePaths = new Map();
for (const file of ROUTERS) {
  for (const line of readFileSync(file, 'utf8').split(NL)) {
    const i = line.indexOf('HandleFunc(' + Q);
    if (i < 0) continue;
    const rest = line.slice(i + 12);
    const j = rest.indexOf(Q);
    if (j > 0) codePaths.set(rest.slice(0, j), file);
  }
}

const undeclared = [...codePaths.keys()].filter((p) => !contractPaths.has(p));
const ghosts = [...contractPaths].filter((p) => !codePaths.has(p));

if (undeclared.length === 0 && ghosts.length === 0) {
  console.log('✓ 路由清单与契约一致（' + codePaths.size + ' 条）');
  process.exit(0);
}
if (undeclared.length) {
  console.log('未登记到契约的路由（先写 contract.yaml 再写代码）：');
  for (const p of undeclared) console.log('  ' + p + '  (' + codePaths.get(p) + ')');
}
if (ghosts.length) {
  console.log('契约里有、代码里没有（幽灵接口）：');
  for (const p of ghosts) console.log('  ' + p);
}
process.exit(strict ? 1 : 0);
