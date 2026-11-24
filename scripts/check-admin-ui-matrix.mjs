#!/usr/bin/env node
// ADM-UI-00 断言脚本：页面矩阵必须可追踪、可复核、证据真实存在。
//
// 用法: node scripts/check-admin-ui-matrix.mjs
//
// 校验的是 docs/impl/evidence/ADM-UI-00/page-matrix.md 的结构与证据，
// 而不是"看起来写了"。矩阵缺行、缺 spec 行号、缺证据、缺缺口卡都会红。
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const EV = path.join(ROOT, 'docs/impl/evidence/ADM-UI-00');
const MATRIX = path.join(EV, 'page-matrix.md');
const MANIFEST = path.join(EV, 'screens/manifest.json');
const PLAN = path.join(ROOT, 'docs/impl/web-admin-redesign-plan-v0.1.md');
const TASKS = path.join(ROOT, 'docs/impl/tasks');

const STATUS = new Set(['implemented', 'partial', 'missing', 'blocked-by-api', 'blocked-by-security', 'n/a']);
const REQUIRED_COLS = ['ID', '页面', 'spec', '原型', '实现', '状态', '证据', '缺口卡'];
const VIEWPORT = { width: 1360, height: 860 };
const SCREENS = ['a1-login', 'a2-dashboard', 'a3-users', 'a6-device-detail', 'a7-sessions', 'a12-config'];

const fails = [];
const fail = (msg) => fails.push(msg);

function readMatrix() {
  if (!fs.existsSync(MATRIX)) {
    fail('缺少矩阵文件 docs/impl/evidence/ADM-UI-00/page-matrix.md');
    return null;
  }
  return fs.readFileSync(MATRIX, 'utf8');
}

function splitRow(line) {
  return line.split('|').slice(1, -1).map((c) => c.trim());
}

// 矩阵由多张同构表组成（A1-A14、横向技术栈），每张都要按同一套规则校验；
// 只校验第一张表会漏掉一半行。
function parseTable(text) {
  const lines = text.split('\n');
  const rows = [];
  let header = null;
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i].trim();
    const isHeader = line.startsWith('|') && line.includes('状态') && line.includes('spec');
    if (isHeader) {
      header = splitRow(line);
      i++; // 跳过 |---| 分隔行
      continue;
    }
    if (!header || !line.startsWith('|')) {
      if (!line.startsWith('|')) header = null;
      continue;
    }
    const cells = splitRow(line);
    if (cells.length !== header.length) {
      fail('列数不匹配: ' + line.slice(0, 80));
      continue;
    }
    const row = {};
    header.forEach((h, idx) => { row[h] = cells[idx]; });
    rows.push(row);
  }
  if (rows.length === 0) fail('矩阵里找不到包含 spec/状态 的表头');
  return rows;
}

function pngSize(file) {
  const buf = fs.readFileSync(file);
  if (buf.length < 24 || buf.toString('ascii', 1, 4) !== 'PNG') return null;
  return { width: buf.readUInt32BE(16), height: buf.readUInt32BE(20) };
}

function cardExists(id) {
  if (!id || id === '—' || id === '-') return true;
  if (fs.existsSync(path.join(TASKS, id + '.md'))) return true;
  // 卡号允许带后缀（ADM-API-03-user-contract.md），按 "<id>-" 前缀匹配。
  if (fs.existsSync(TASKS) && fs.readdirSync(TASKS).some((f) => f.startsWith(id + '-'))) return true;
  if (fs.existsSync(PLAN) && fs.readFileSync(PLAN, 'utf8').includes(id)) return true;
  return false;
}

function evidenceExists(cell) {
  const tokens = cell.split(/[;,、\s]+/).filter(Boolean);
  const paths = tokens.filter((t) => /^(web-admin\/|server\/|scripts\/|docs\/|evidence\/)/.test(t.replace(/:\d+(-\d+)?$/, '')));
  for (const raw of paths) {
    const clean = raw.replace(/:\d+(-\d+)?$/, '').replace(/:Test.*$/, '');
    const candidates = [path.join(ROOT, clean), path.join(EV, clean)];
    if (!candidates.some((p) => fs.existsSync(p))) fail('证据路径不存在: ' + raw);
  }
}

function report(rowCount) {
  if (fails.length) {
    console.error('FAIL ADM-UI-00 矩阵断言');
    for (const f of fails) console.error('  - ' + f);
    process.exit(1);
  }
  console.log('PASS TestAdminUiMatrixRowsComplete: ' + rowCount + ' 行，A1-A14 与 s12-s17 全覆盖');
  console.log('PASS TestAdminUiMatrixEvidenceExists: 证据路径与 6 张 1360x860 截图均存在');
}

function main() {
  const text = readMatrix();
  if (!text) return report(0);

  const rows = parseTable(text);
  if (rows.length === 0) fail('矩阵没有数据行');
  const header = rows.length ? Object.keys(rows[0]) : [];
  for (const col of REQUIRED_COLS) {
    if (!header.includes(col)) fail('缺列: ' + col);
  }
  if (fails.length) return report(rows.length);

  const seen = new Map();
  const prototypeScreens = new Set();
  for (const row of rows) {
    const id = row.ID;
    if (!id) { fail('空 ID 行'); continue; }
    if (seen.has(id)) fail('重复行: ' + id);
    seen.set(id, row);

    // A 行是页面：必须同时有 pages.md 与 PRD 行号。X 行是技术栈：只在 PRD §6.4/§6.7 有定义。
    if (id.startsWith('A') && !/pages\.md:\d+(-\d+)?/.test(row.spec)) fail(id + ' 缺 pages.md 行号: ' + row.spec);
    if (!/PRD:\d+(-\d+)?/.test(row.spec)) fail(id + ' 缺 PRD 行号: ' + row.spec);
    if (!STATUS.has(row['状态'])) fail(id + ' 状态非法: ' + row['状态']);

    const impl = row['实现'];
    if (!['missing', 'blocked-by-api', 'n/a'].includes(row['状态'])) {
      if (!/(web-admin|server|prototype|scripts)\/[^\s;]+:\d+/.test(impl)) fail(id + ' 缺代码文件行号: ' + impl);
    }

    const ev = row['证据'];
    if (['implemented', 'partial'].includes(row['状态']) && (!ev || ev === '—')) {
      fail(id + ' 状态为 ' + row['状态'] + ' 但没有任何证据');
    }
    if (ev && ev !== '—') evidenceExists(ev);

    if (['missing', 'blocked-by-api', 'blocked-by-security'].includes(row['状态']) && !cardExists(row['缺口卡'])) {
      fail(id + ' 需要缺口卡但填的是: ' + row['缺口卡']);
    }
    if (row['缺口卡'] && row['缺口卡'] !== '—' && !cardExists(row['缺口卡'])) {
      fail(id + ' 缺口卡不存在: ' + row['缺口卡']);
    }
    for (const m of row['原型'].matchAll(/s\d{2}/g)) prototypeScreens.add(m[0]);
  }

  for (let i = 1; i <= 14; i++) {
    if (!seen.has('A' + i)) fail('缺页面行 A' + i);
  }
  for (let i = 1; i <= 6; i++) {
    if (!seen.has('X' + i)) fail('缺横向技术栈行 X' + i);
  }
  for (const s of ['s12', 's13', 's14', 's15', 's16', 's17']) {
    if (!prototypeScreens.has(s)) fail('原型屏未进矩阵: ' + s);
  }

  if (!fs.existsSync(MANIFEST)) {
    fail('缺少截图清单 docs/impl/evidence/ADM-UI-00/screens/manifest.json');
  } else {
    let manifest;
    try {
      manifest = JSON.parse(fs.readFileSync(MANIFEST, 'utf8'));
    } catch (err) {
      fail('manifest.json 不是合法 JSON: ' + err.message);
      return report(rows.length);
    }
    if (manifest.viewport?.width !== VIEWPORT.width || manifest.viewport?.height !== VIEWPORT.height) {
      fail('manifest viewport 必须是 1360x860，实际: ' + JSON.stringify(manifest.viewport));
    }
    const shots = manifest.screens ?? [];
    for (const name of SCREENS) {
      const shot = shots.find((s) => s.name === name);
      if (!shot) { fail('manifest 缺截图: ' + name); continue; }
      const file = path.join(EV, 'screens', shot.file);
      if (!fs.existsSync(file)) { fail('截图文件不存在: ' + shot.file); continue; }
      const size = pngSize(file);
      if (!size) { fail('不是 PNG: ' + shot.file); continue; }
      if (size.width !== VIEWPORT.width || size.height !== VIEWPORT.height) {
        fail(shot.file + ' 尺寸 ' + size.width + 'x' + size.height + '，要求 1360x860');
      }
    }
    if ((manifest.pageErrors ?? []).length > 0) fail('截图采集期间有 pageerror: ' + JSON.stringify(manifest.pageErrors));
    if (!manifest.fixture || !manifest.font) fail('manifest 必须记录 fixture 与字体');
  }

  return report(rows.length);
}

main();
