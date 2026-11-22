#!/usr/bin/env node
// 禁止模式扫描：httpx 绕过、网关裸 map、硬编码 endpoint、空 catch、固定 sleep、
// FFI 字符串指针不配对。存量按「规则 + 文件」计数写入 scripts/gate-baseline.json，
// 只允许减少，不允许新增。
//
// 用法: node scripts/check-forbidden.mjs [--report|--update-baseline]
import { readFileSync, readdirSync, statSync, writeFileSync } from 'node:fs';
import { join, relative, sep } from 'node:path';

export const RULES = [
  {
    id: 'httpx-bypass',
    description: '响应必须走 httpx.OK/OKPage/Err',
    applies: (p) => p.endsWith('.go') && !p.endsWith('_test.go') && p !== 'server/internal/httpx/httpx.go',
    count: (text) => countLines(text, /json\.NewEncoder|http\.Error\(|w\.Write\(\[\]byte/),
  },
  {
    id: 'bare-map',
    description: '后台 handler 不得直接返回 map[string]any',
    applies: (p) =>
      p.endsWith('.go') &&
      !p.endsWith('_test.go') &&
      (p.startsWith('server/internal/adminhttp/') || p.startsWith('server/internal/gateway/')),
    count: (text) => countLines(text, /map\[string\]any/),
  },
  {
    id: 'hardcoded-endpoint',
    description: '生产 UI 不得硬编码 host/端口/协议',
    applies: (p) => p.startsWith('client/lib/') && p.endsWith('.dart'),
    count: (text) => countLines(text, /127\.0\.0\.1|localhost|0\.0\.0\.0|:\d{4,5}\b|https?:\/\/|wss?:\/\//),
  },
  {
    id: 'empty-catch',
    description: '空 catch 必须显式处理或注明理由并有计数器',
    applies: (p) => p.startsWith('client/lib/') && p.endsWith('.dart'),
    count: (text) => (text.match(/catch\s*\([^)]*\)\s*\{\s*\}/g) || []).length,
  },
  {
    id: 'fixed-sleep',
    description: '禁止用固定 sleep 协调握手/授权/状态迁移',
    applies: (p) =>
      (p.startsWith('client/lib/') && p.endsWith('.dart')) ||
      (p.startsWith('server/internal/') && p.endsWith('.go') && !p.endsWith('_test.go')),
    count: (text) =>
      (text.match(/Future(?:<[^>]*>)?\s*\.\s*delayed\s*\(/g) || []).length +
      (text.match(/time\.Sleep\s*\(/g) || []).length,
  },
  {
    id: 'ffi-pointer-balance',
    description: 'toNativeUtf8 必须由 calloc.free 配对释放',
    applies: (p) => p.startsWith('client/lib/') && p.endsWith('.dart'),
    count: (text) => {
      const allocs = (text.match(/toNativeUtf8\s*\(/g) || []).length;
      const frees = (text.match(/calloc\.free\s*\(/g) || []).length;
      return Math.max(0, allocs - frees);
    },
  },
];

function countLines(text, re) {
  let n = 0;
  for (const line of text.split('\n')) {
    // 纯注释行不参与：文档里出现示例 URL/旧写法不应算违规。
    const t = line.trim();
    if (t.startsWith('//') || t.startsWith('#') || t.startsWith('/*') || t.startsWith('*')) continue;
    if (re.test(line)) n++;
  }
  return n;
}

/** 对内存中的文件集合跑规则（自测入口）。 */
export function scanFiles(files, rules = RULES) {
  const counts = new Map();
  for (const file of files) {
    const p = file.path.split(sep).join('/');
    for (const rule of rules) {
      if (!rule.applies(p)) continue;
      const n = rule.count(file.content, p);
      if (n > 0) counts.set(rule.id + ':' + p, n);
    }
  }
  return counts;
}

export function serializeBaseline(counts) {
  const obj = {};
  for (const key of [...counts.keys()].sort()) obj[key] = counts.get(key);
  return JSON.stringify({ version: 1, counts: obj }, null, 2) + '\n';
}

export function parseBaseline(text) {
  const raw = JSON.parse(text);
  if (raw.version !== 1 || typeof raw.counts !== 'object' || raw.counts === null) {
    throw new Error('基线格式非法（需要 version=1 与 counts）');
  }
  const out = new Map();
  for (const [k, v] of Object.entries(raw.counts)) {
    if (!Number.isInteger(v) || v < 0) throw new Error('基线计数非法: ' + k);
    out.set(k, v);
  }
  return out;
}

function walk(dir, out = []) {
  let entries;
  try {
    entries = readdirSync(dir);
  } catch {
    return out;
  }
  for (const name of entries) {
    if (name === 'node_modules' || name === '.dart_tool' || name === 'build') continue;
    const abs = join(dir, name);
    const st = statSync(abs);
    if (st.isDirectory()) walk(abs, out);
    else out.push(abs);
  }
  return out;
}

function main() {
  const ROOT = process.cwd();
  const BASELINE = join(ROOT, 'scripts/gate-baseline.json');
  const args = new Set(process.argv.slice(2));
  const files = ['client/lib', 'server/internal', 'server/cmd']
    .flatMap((d) => walk(join(ROOT, d)))
    .map((abs) => ({ path: relative(ROOT, abs).split(sep).join('/'), content: readFileSync(abs, 'utf8') }));
  const counts = scanFiles(files);

  if (args.has('--update-baseline')) {
    writeFileSync(BASELINE, serializeBaseline(counts));
    console.log('已更新基线：scripts/gate-baseline.json（' + counts.size + ' 条规则/文件组合）');
    return 0;
  }

  if (args.has('--report')) {
    console.log('禁止模式清单（报告模式，不判红）：');
    for (const key of [...counts.keys()].sort()) {
      console.log('  ' + key + ' × ' + counts.get(key));
    }
    console.log('合计 ' + counts.size + ' 条规则/文件组合');
    return 0;
  }

  let base;
  try {
    base = parseBaseline(readFileSync(BASELINE, 'utf8'));
  } catch (e) {
    console.error('无法读取基线 scripts/gate-baseline.json：' + e.message);
    console.error('负责人批准后执行：bash scripts/gate.sh --update-baseline');
    return 1;
  }

  const added = [];
  for (const [key, n] of counts) {
    const allowed = base.get(key) || 0;
    if (n > allowed) added.push({ key, n, allowed });
  }
  if (added.length > 0) {
    console.error('新增违规（迁移完成前不许再扩散）：');
    for (const a of added) {
      console.error('  ' + a.key + ' 新增 ' + (a.n - a.allowed) + ' 处（基线 ' + a.allowed + '）');
    }
    return 1;
  }
  console.log('无新增违规（存量 ' + counts.size + ' 条规则/文件组合待清零）');
  return 0;
}

if (process.argv[1] && process.argv[1].endsWith('check-forbidden.mjs')) {
  process.exit(main());
}
