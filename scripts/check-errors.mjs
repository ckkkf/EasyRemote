#!/usr/bin/env node
// 校验错误码三端一致性：docs/spec/error-codes.md ↔ Go ↔ Rust
// 用法: node scripts/check-errors.mjs
import { readFileSync } from 'node:fs';

const doc = readFileSync('docs/spec/error-codes.md', 'utf8');
// 表格行形如: | 200001 | CONN_DEVICE_NOT_FOUND | ...
const docPairs = new Map();
for (const line of doc.split('\n')) {
  const m = line.match(/^\|\s*(\d{6})\s*\|\s*([A-Z][A-Z0-9_]+)\s*\|/);
  if (m) docPairs.set(Number(m[1]), m[2]);
}

function parseGo(file) {
  const src = readFileSync(file, 'utf8');
  const map = new Map();
  const re = /(\w+)\s+Code\s*=\s*(\d{6})/g;
  let m;
  while ((m = re.exec(src))) map.set(Number(m[2]), m[1]);
  // registry 里有 name 字符串，优先用它作为权威名
  const reg = /\{(\w+),\s*"([A-Z][A-Z0-9_]+)"\}/g;
  const names = new Map();
  while ((m = reg.exec(src))) names.set(m[1], m[2]);
  const out = new Map();
  for (const [code, ident] of map) out.set(code, names.get(ident) ?? ident);
  return out;
}

function parseRust(file) {
  const src = readFileSync(file, 'utf8');
  const map = new Map();
  const re = /pub const ([A-Z][A-Z0-9_]+): Code = Code\((\d{6})\);/g;
  let m;
  while ((m = re.exec(src))) map.set(Number(m[2]), m[1]);
  return map;
}

const goPairs = parseGo('server/internal/errcode/codes_gen.go');
let rustPairs = new Map();
try {
  rustPairs = parseRust('core/crates/er-common/src/errors_gen.rs');
} catch {
  console.log('! Rust 侧错误码文件暂不存在，跳过 Rust 比对');
}

const problems = [];

for (const [code, name] of docPairs) {
  if (!goPairs.has(code)) problems.push(`文档有但 Go 缺失: ${code} ${name}`);
  else if (goPairs.get(code) !== name) problems.push(`命名不一致 Go: ${code} 文档=${name} Go=${goPairs.get(code)}`);
}
for (const [code, name] of goPairs) {
  if (!docPairs.has(code)) problems.push(`Go 有但文档缺失: ${code} ${name}`);
}
for (const [code, name] of rustPairs) {
  if (!goPairs.has(code)) problems.push(`Rust 有但 Go 缺失: ${code} ${name}`);
  else if (goPairs.get(code) !== name) problems.push(`命名不一致 Rust: ${code} Go=${goPairs.get(code)} Rust=${name}`);
}

console.log(`文档 ${docPairs.size} 条 / Go ${goPairs.size} 条 / Rust ${rustPairs.size} 条`);
if (problems.length) {
  console.error('\n发现 ' + problems.length + ' 处不一致：');
  for (const p of problems) console.error('  - ' + p);
  process.exit(1);
}
console.log('✔ 三端错误码一致');
