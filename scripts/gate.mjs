#!/usr/bin/env node
// EasyRemote 严格闸门：本地与 CI 共用的唯一命令清单。
//
// 为什么用 JS 而不是继续堆 bash：闸门需要被自测（TestGateMatchesCIJobs /
// TestGateFailsWhenAnyStackFails）、需要按 stack 汇总，并且 CI 必须和本地跑
// 同一组命令。步骤只在这里定义一次。
//
// 用法:
//   bash scripts/gate.sh                    严格模式；任一步红即退出 1
//   bash scripts/gate.sh --report           只报告，不判红（不能作为完成证明）
//   bash scripts/gate.sh --update-baseline  更新禁止模式基线（需负责人批准）
//   bash scripts/gate.sh --only=go-test,rust-clippy
//   bash scripts/gate.sh --list
import { spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..');

// cargo 不保证在 PATH（本机 rustup 装了 toolchain 但没装 shim），先解析再前置。
const CARGO = 'export PATH="$(dirname "$(rustup which cargo 2>/dev/null || command -v cargo)"):$PATH"; ';

// flutter analyze 的失败判定：error/warning 必须为 0；info 是存量债务（单独卡收敛）。
export function evaluateAnalyze({ output }) {
  const errors = [];
  const infos = [];
  for (const line of output.split('\n')) {
    if (/^\s*error\s+•/.test(line)) errors.push(line.trim());
    else if (/^\s*warning\s+•/.test(line)) errors.push(line.trim());
    else if (/^\s*info\s+•/.test(line)) infos.push(line.trim());
  }
  if (errors.length > 0) {
    return { ok: false, note: errors.length + ' 个 error/warning' };
  }
  return { ok: true, note: infos.length + ' 个存量 info' };
}

// 步骤清单：id 唯一；stack 用于汇总；捕获输出的步骤才能带 evaluate。
export const STEPS = [
  { id: 'scripts-selftest', stack: 'scripts', name: '闸门自测', cmd: 'node --test scripts/tests/*.test.mjs' },
  { id: 'secret-scan', stack: 'scripts', name: '高置信 secret 扫描', cmd: 'node scripts/check-secrets.mjs' },
  {
    id: 'contract-gen',
    stack: 'contract',
    name: '生成物一致性（错误码/契约/能力台账）',
    cmd: 'node scripts/gen-errors.mjs --check && node scripts/gen-contract.mjs --check && node scripts/gen-capabilities.mjs --check',
  },
  {
    id: 'routes',
    stack: 'contract',
    name: '路由清单 vs 契约',
    cmd: 'node scripts/check-routes.mjs --strict',
    cmdReport: 'node scripts/check-routes.mjs',
  },
  {
    id: 'forbidden',
    stack: 'contract',
    name: '禁止模式（httpx / 裸 map / 硬编码 / 空 catch / 固定 sleep / FFI 指针）',
    cmd: 'node scripts/check-forbidden.mjs',
    cmdReport: 'node scripts/check-forbidden.mjs --report',
  },
  { id: 'version', stack: 'contract', name: '版本一致性', cmd: 'node scripts/check-version.mjs' },
  {
    id: 'gofmt',
    stack: 'go',
    name: 'gofmt',
    cmd: 'out=$(gofmt -l server); if [ -n "$out" ]; then echo "$out"; exit 1; fi',
  },
  { id: 'go-vet', stack: 'go', name: 'go vet', cmd: 'cd server && go vet ./...' },
  { id: 'go-test', stack: 'go', name: 'go test -race', cmd: 'cd server && go test -race ./...' },
  {
    id: 'go-envelope',
    stack: 'go',
    name: '响应信封契约（严格）',
    cmd: 'cd server && ER_ENVELOPE_STRICT=1 go test ./internal/adminhttp/ -run TestAdminEnvelopeContract',
    cmdReport: 'cd server && go test ./internal/adminhttp/ -run TestAdminEnvelopeContract',
  },
  {
    id: 'rust-fmt',
    stack: 'rust',
    name: 'cargo fmt --check',
    cmd: CARGO + 'cd core && cargo fmt --all -- --check',
  },
  {
    id: 'rust-clippy',
    stack: 'rust',
    name: 'cargo clippy -D warnings',
    cmd: CARGO + 'cd core && cargo clippy --workspace --all-targets -- -D warnings',
  },
  {
    id: 'rust-test',
    stack: 'rust',
    name: 'cargo test',
    cmd: CARGO + 'cd core && cargo test --workspace',
  },
  {
    id: 'rust-ffi-cdylib',
    stack: 'rust',
    name: 'cargo build er-ffi（E2E 需要 cdylib）',
    // flutter test 的桌面 E2E 通过 ../core/target/debug/liber_ffi.dylib 加载核心；
    // cargo test 不会产出 cdylib，必须在 flutter 之前显式构建。
    cmd: CARGO + 'cd core && cargo build -p er-ffi',
  },
  {
    id: 'flutter-format',
    stack: 'flutter',
    name: 'dart format --set-exit-if-changed',
    // 生成物（contract.dart / *_gen.dart）由生成器保证内容，不能被人肉格式化，
    // 否则 --check 会与生成器输出的空白风格冲突。
    cmd: 'cd client && find lib test -name \'*.dart\' ! -name \'*_gen.dart\' ! -name \'contract.dart\' -print0 | xargs -0 dart format --output=none --set-exit-if-changed',
  },
  {
    id: 'flutter-analyze',
    stack: 'flutter',
    name: 'flutter analyze（error/warning=0）',
    cmd: 'cd client && flutter analyze',
    capture: true,
    evaluate: evaluateAnalyze,
  },
  {
    id: 'flutter-test',
    stack: 'flutter',
    name: 'flutter test',
    // 桌面 E2E 需要真实 FFI/显示：macOS/Windows 本地全跑，Linux 只排除该标签。
    // 客户端测试串行执行：E2E 会构建并启动真实服务，多个 suite 并发会互相抢 CPU/端口。
    cmd: 'cd client && if [ "$(uname -s)" = "Linux" ]; then flutter test --concurrency=1 --exclude-tags e2e-desktop; else flutter test --concurrency=1; fi',
  },
  { id: 'web-tsc', stack: 'web', name: 'web-admin tsc --noEmit', cmd: 'cd web-admin && npx tsc --noEmit' },
  { id: 'web-test', stack: 'web', name: 'web-admin npm test', cmd: 'cd web-admin && npm test -- --run' },
  { id: 'web-build', stack: 'web', name: 'web-admin build', cmd: 'cd web-admin && npm run build' },
];

// CI 必须覆盖的关键命令（TestGateMatchesCIJobs 会逐条核对 ci.yml 与本清单）。
export const REQUIRED_COMMANDS = {
  scripts: ['node --test scripts/tests/*.test.mjs'],
  contract: ['gen-errors', 'gen-contract', 'gen-capabilities', 'check-routes', 'check-version', 'check-forbidden'],
  go: ['go vet', 'go test -race', 'ER_ENVELOPE_STRICT=1'],
  rust: ['cargo fmt', 'cargo clippy', 'cargo test', '-D warnings'],
  flutter: ['dart format', 'flutter analyze', 'flutter test'],
  web: ['tsc --noEmit', 'npm test', 'npm run build'],
};

function defaultRun(step, { root, mode }) {
  const cmd = (mode === 'report' && step.cmdReport) || step.cmd;
  const started = Date.now();
  const res = spawnSync('bash', ['-lc', cmd], {
    cwd: root,
    encoding: 'utf8',
    stdio: step.capture ? 'pipe' : 'inherit',
  });
  return {
    code: res.status === null ? 1 : res.status,
    output: step.capture ? (res.stdout || '') + (res.stderr || '') : '',
    durationMs: Date.now() - started,
  };
}

/**
 * 执行一组成员步骤。
 *
 * @param {{steps?: Array, root?: string, mode?: 'strict'|'report', only?: string[], run?: Function, log?: Function}} opts
 */
export function runGate(opts = {}) {
  const steps = opts.steps || STEPS;
  const mode = opts.mode || 'strict';
  const only = opts.only && opts.only.length > 0 ? new Set(opts.only) : null;
  const root = opts.root || ROOT;
  const run = opts.run || defaultRun;
  const log = opts.log || ((line) => process.stdout.write(line + '\n'));

  const selected = steps.filter((s) => !only || only.has(s.id));
  const results = [];
  selected.forEach((step, i) => {
    log('');
    log('== ' + (i + 1) + '/' + selected.length + ' ' + step.stack + ': ' + step.name + ' ==');
    const out = run(step, { root, mode });
    let ok = out.code === 0;
    let note = '';
    if (step.evaluate) {
      const verdict = step.evaluate({ ...out, step });
      ok = verdict.ok;
      note = verdict.note || '';
    }
    if (step.capture && out.output) log(out.output.trimEnd());
    if (note) log('  → ' + note);
    log(ok ? '  PASS' : '  FAIL (exit ' + out.code + ')');
    results.push({ id: step.id, stack: step.stack, name: step.name, ok, code: out.code, note, durationMs: out.durationMs || 0 });
  });

  const stacks = [];
  for (const r of results) if (!stacks.includes(r.stack)) stacks.push(r.stack);
  log('');
  log('== 汇总 ==');
  for (const stack of stacks) {
    const rows = results.filter((r) => r.stack === stack);
    const bad = rows.filter((r) => !r.ok);
    log('  ' + stack.padEnd(10) + (bad.length === 0 ? 'PASS' : 'FAIL (' + bad.map((r) => r.id).join(', ') + ')'));
  }
  const failed = results.filter((r) => !r.ok);
  const ok = mode === 'report' ? true : failed.length === 0;
  log('');
  log('GATE: ' + (ok ? 'PASS' : 'FAIL'));
  if (mode === 'report' && failed.length > 0) {
    log('报告模式：' + failed.length + ' 个步骤失败（不判红，不能作为完成证明）');
  }
  return { ok, results, failed };
}

function parseArgs(argv) {
  const opts = { mode: 'strict', only: [], list: false, updateBaseline: false };
  for (const arg of argv) {
    if (arg === '--report') opts.mode = 'report';
    else if (arg === '--list') opts.list = true;
    else if (arg === '--update-baseline') opts.updateBaseline = true;
    else if (arg.startsWith('--only=')) opts.only = arg.slice('--only='.length).split(',').filter(Boolean);
    else if (arg === '--help' || arg === '-h') opts.help = true;
    else {
      process.stderr.write('未知参数: ' + arg + '\n');
      opts.mode = 'error';
    }
  }
  return opts;
}

function main() {
  const opts = parseArgs(process.argv.slice(2));
  if (opts.help) {
    process.stdout.write(readFileSync(fileURLToPath(import.meta.url), 'utf8').split('\n').slice(2, 18).join('\n') + '\n');
    return 0;
  }
  if (opts.mode === 'error') return 2;
  if (opts.list) {
    for (const s of STEPS) process.stdout.write(s.stack.padEnd(10) + s.id.padEnd(18) + s.name + '\n');
    return 0;
  }
  if (opts.updateBaseline) {
    const res = spawnSync('node', ['scripts/check-forbidden.mjs', '--update-baseline'], { cwd: ROOT, stdio: 'inherit' });
    return res.status === null ? 1 : res.status;
  }
  return runGate({ mode: opts.mode, only: opts.only }).ok ? 0 : 1;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  process.exit(main());
}
