#!/usr/bin/env node
// 版本一致性校验：一个项目的版本号散落在多处，发布前必须全部对齐。
//
// 用法:
//   node scripts/check-version.mjs                 # 打印各处版本并校验是否一致
//   node scripts/check-version.mjs --tag v1.2.3    # 校验 tag 与各处版本一致（CI 用）
//   node scripts/check-version.mjs --set 1.2.3     # 把各处版本统一改为 1.2.3，并打印建议的 tag
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');

const SOURCES = [
  {
    name: 'VERSION',
    file: 'VERSION',
    read: (text) => text.trim(),
    write: (v) => v + '\n',
  },
  {
    name: 'core/Cargo.toml (workspace.package.version)',
    file: 'core/Cargo.toml',
    re: /(^\[workspace\.package\][\s\S]*?^version\s*=\s*)"([^"]+)"/m,
    group: 2,
  },
  {
    name: 'web-admin/package.json',
    file: 'web-admin/package.json',
    re: /("version"\s*:\s*)"([^"]+)"/,
    group: 2,
  },
  {
    name: 'client/pubspec.yaml',
    file: 'client/pubspec.yaml',
    // pubspec 的版本行不带引号，可带 +build 后缀
    re: /(^version:\s*)([0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?)(\+[0-9]+)?/m,
    group: 2,
    quote: false,
  },
];

const SEMVER = /^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$/;

function readVersion(src) {
  const abs = path.join(ROOT, src.file);
  if (!fs.existsSync(abs)) return { ok: false, reason: '文件不存在' };
  const text = fs.readFileSync(abs, 'utf8');
  if (src.read) return { ok: true, value: src.read(text) };
  const m = text.match(src.re);
  if (!m) return { ok: false, reason: '未找到版本字段' };
  return { ok: true, value: m[src.group] };
}

function writeVersion(src, version) {
  const abs = path.join(ROOT, src.file);
  if (src.write) {
    fs.writeFileSync(abs, src.write(version));
    return true;
  }
  const text = fs.readFileSync(abs, 'utf8');
  const q = src.quote === false ? '' : '"';
  const next = text.replace(src.re, (all, pre, _old, suffix) =>
    pre + q + version + q + (suffix || ''),
  );
  if (next === text) return false;
  fs.writeFileSync(abs, next);
  return true;
}

const args = process.argv.slice(2);
const tagIdx = args.indexOf('--tag');
const setIdx = args.indexOf('--set');

// ---------------------------------------------------------------- --set
if (setIdx !== -1) {
  const raw = (args[setIdx + 1] || '').replace(/^v/, '');
  if (!SEMVER.test(raw)) {
    console.error('用法: node scripts/check-version.mjs --set 1.2.3');
    process.exit(2);
  }
  if (!fs.existsSync(path.join(ROOT, 'VERSION'))) {
    fs.writeFileSync(path.join(ROOT, 'VERSION'), '0.0.0\n');
  }
  for (const s of SOURCES) {
    const ok = writeVersion(s, raw);
    console.log((ok ? '✓ ' : '✗ ') + s.name + ' -> ' + raw);
  }
  console.log('');
  console.log('版本已统一为 ' + raw + '。');
  console.log('下一步：bash scripts/gate.sh && git commit -am "chore(release): v' + raw + '"');
  console.log('打 Tag：git tag v' + raw + ' && git push origin v' + raw);
  process.exit(0);
}

// ---------------------------------------------------------------- 读取
const found = [];
let bad = false;
for (const s of SOURCES) {
  const r = readVersion(s);
  if (!r.ok) {
    console.log('✗ ' + s.name + ': ' + r.reason);
    bad = true;
    continue;
  }
  found.push({ name: s.name, value: r.value });
  console.log('  ' + s.name + ': ' + r.value);
}

const values = [...new Set(found.map((f) => f.value))];
let exitCode = bad ? 1 : 0;

if (values.length > 1) {
  console.error('');
  console.error('❌ 各处版本号不一致：' + values.join(' / '));
  console.error('   修复：node scripts/check-version.mjs --set <版本>');
  exitCode = 1;
} else if (values.length === 1) {
  console.log('');
  console.log('✓ 各处版本一致：' + values[0]);
}

// ---------------------------------------------------------------- --tag
if (tagIdx !== -1) {
  const tag = args[tagIdx + 1] || '';
  const tagVersion = tag.replace(/^v/, '');
  const current = values[0];
  if (!tag) {
    console.error('❌ --tag 需要一个参数，例如 --tag v1.0.0');
    exitCode = 2;
  } else if (tagVersion !== current) {
    console.error('❌ Tag ' + tag + ' 与包版本 ' + current + ' 不一致');
    console.error('   要么先升版本：node scripts/check-version.mjs --set ' + tagVersion);
    console.error('   要么改打 v' + current + ' 的 Tag。');
    exitCode = 1;
  } else {
    console.log('✓ Tag ' + tag + ' 与包版本一致');
  }
}

if (bad && exitCode === 0) exitCode = 1;
process.exit(exitCode);
