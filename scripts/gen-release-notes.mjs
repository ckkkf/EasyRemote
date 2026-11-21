#!/usr/bin/env node
// 生成 Release 发布说明：按类型归类 Tag 区间的提交，并列出产物与校验方法。
// 环境变量：VERSION / GATEWAY / GITHUB_REPOSITORY
import fs from 'node:fs';
import path from 'node:path';
import { execSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const version = process.env.VERSION || '';
const gateway = process.env.GATEWAY || '';
const repo = process.env.GITHUB_REPOSITORY || '';
const BT = String.fromCharCode(96);
const code = (s) => BT + s + BT;
const FENCE = BT + BT + BT;

function sh(cmd) {
  try { return execSync(cmd, { cwd: ROOT, encoding: 'utf8' }).trim(); } catch { return ''; }
}

const tags = sh('git tag --sort=-creatordate').split(String.fromCharCode(10)).filter((t) => t && t !== 'v' + version);
const from = tags[0] || '';
const range = from ? from + '..HEAD' : 'HEAD';
const log = sh('git log ' + range + ' --no-merges --pretty=format:%s').split(String.fromCharCode(10)).filter(Boolean);

const GROUPS = [
  { key: 'feat', title: '新功能' },
  { key: 'fix', title: '修复' },
  { key: 'perf', title: '性能' },
  { key: 'refactor', title: '重构' },
  { key: 'docs', title: '文档' },
  { key: 'ci', title: 'CI / 构建' },
  { key: 'test', title: '测试' },
  { key: 'chore', title: '其他维护' },
];

const buckets = new Map();
const others = [];
for (const line of log) {
  const m = line.match(/^(\w+)(?:\(([^)]+)\))?!?:\s*(.+)$/);
  if (m && GROUPS.some((g) => g.key === m[1])) {
    const text = m[2] ? '**' + m[2] + '**: ' + m[3] : m[3];
    if (!buckets.has(m[1])) buckets.set(m[1], []);
    buckets.get(m[1]).push(text);
  } else {
    others.push(line);
  }
}

const releaseDir = path.join(ROOT, 'release');
let assets = [];
try {
  assets = fs.readdirSync(releaseDir).filter((f) => f !== 'SHA256SUMS.txt' && f !== 'RELEASE_NOTES.md');
} catch { assets = []; }

const L = [];
L.push('# EasyRemote v' + version, '');
if (gateway) {
  L.push('> 本次发布的客户端已内置服务端地址：' + code(gateway), '');
} else {
  L.push('> 本次发布未内置服务端地址：Web 版走同源相对路径，桌面版默认 ' + code('127.0.0.1:8080') + '，可在 App 设置中修改。', '');
}

L.push('## 下载', '', '| 产物 | 平台 | 说明 |', '|---|---|---|');
for (const a of assets.sort()) {
  let platform = '—', desc = '';
  if (a.indexOf('client-web') !== -1) { platform = 'Web'; desc = '解压后用静态服务器托管，或用 ' + code('scripts/serve-client-web.mjs'); }
  else if (a.indexOf('client-macos') !== -1) { platform = 'macOS (Apple Silicon)'; desc = '解压得到 .app；首次打开若提示未验证，右键 → 打开'; }
  else if (a.indexOf('server-linux') !== -1) { platform = 'Linux 服务端'; desc = '网关 / 中继 / rendezvous 等 7 个入口'; }
  else if (a.indexOf('server-darwin') !== -1) { platform = 'macOS 服务端'; desc = '同上'; }
  else if (a.indexOf('server-windows') !== -1) { platform = 'Windows 服务端'; desc = '同上'; }
  L.push('| ' + code(a) + ' | ' + platform + ' | ' + desc + ' |');
}
L.push('', '> 客户端是主要交付物；服务端二进制供自托管使用。', '');

L.push('## 变更', '');
let any = false;
for (const g of GROUPS) {
  const items = buckets.get(g.key);
  if (!items || !items.length) continue;
  any = true;
  L.push('### ' + g.title, '');
  for (const it of items) L.push('- ' + it);
  L.push('');
}
if (others.length) {
  any = true;
  L.push('### 其他', '');
  for (const it of others) L.push('- ' + it);
  L.push('');
}
if (!any) L.push('本次发布无功能变更（仅版本号）。', '');

L.push('## 校验', '', FENCE + 'bash', 'sha256sum -c SHA256SUMS.txt', FENCE, '');
if (repo) L.push('完整变更对比：https://github.com/' + repo + '/compare/' + (from || 'HEAD') + '...v' + version, '');

fs.mkdirSync(releaseDir, { recursive: true });
fs.writeFileSync(path.join(releaseDir, 'RELEASE_NOTES.md'), L.join(String.fromCharCode(10)), 'utf8');
console.log('已生成 release/RELEASE_NOTES.md（' + log.length + ' 条提交）');