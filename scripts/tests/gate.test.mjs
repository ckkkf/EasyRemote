// 闸门自测：步骤清单、CI 对照、失败聚合、analyze 判定。
// 运行：node --test scripts/tests/
import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { STEPS, REQUIRED_COMMANDS, evaluateAnalyze, runGate } from '../gate.mjs';

const ROOT = process.cwd();

function stepCommands(steps) {
  return steps.map((s) => (s.cmd || '') + ' ' + (s.cmdReport || '')).join('\n');
}

test('TestGateMatchesCIJobs', () => {
  const ci = readFileSync(join(ROOT, '.github/workflows/ci.yml'), 'utf8');
  const commands = stepCommands(STEPS);
  for (const [stack, tokens] of Object.entries(REQUIRED_COMMANDS)) {
    assert.ok(
      STEPS.some((s) => s.stack === stack),
      '闸门缺少 ' + stack + ' 栈步骤',
    );
    for (const token of tokens) {
      assert.ok(commands.includes(token), '闸门缺少命令：' + stack + ' / ' + token);
      assert.ok(ci.includes(token), 'CI 缺少命令：' + stack + ' / ' + token);
    }
  }
});

test('TestGateFailsWhenAnyStackFails', () => {
  const stacks = [...new Set(STEPS.map((s) => s.stack))];
  assert.ok(stacks.length >= 5, '至少覆盖 scripts/contract/go/rust/flutter/web');
  for (const stack of stacks) {
    const first = STEPS.find((s) => s.stack === stack);
    const result = runGate({
      log: () => {},
      run: (step) => ({ code: step.id === first.id ? 1 : 0, output: '' }),
    });
    assert.equal(result.ok, false, stack + ' 失败时闸门必须判红');
    assert.deepEqual(
      [...new Set(result.failed.map((r) => r.stack))],
      [stack],
      '失败栈必须是 ' + stack,
    );
  }
});

test('TestGateStepsHaveUniqueIds', () => {
  const ids = STEPS.map((s) => s.id);
  assert.equal(new Set(ids).size, ids.length, '步骤 id 必须唯一');
  for (const s of STEPS) {
    assert.ok(s.stack && s.name && (s.cmd || s.run), '步骤字段不完整：' + s.id);
  }
});

test('TestReportModeOnlyReports', () => {
  const result = runGate({
    mode: 'report',
    log: () => {},
    run: (step) => ({ code: 1, output: step.evaluate ? '  error • fake • lib/a.dart:1:1 • x' : '' }),
  });
  assert.equal(result.ok, true, '报告模式不得判红');
  assert.ok(result.failed.length === STEPS.length, '报告模式仍要列出全部失败步骤');
});

test('TestAnalyzeRejectsWarningsButAllowsInfo', () => {
  const infos = evaluateAnalyze({ output: '   info • lint thing • lib/a.dart:1:1 • x' });
  assert.equal(infos.ok, true, '存量 info 不阻塞闸门（单独收敛卡）');
  const warn = evaluateAnalyze({ output: '   warning • real problem • lib/a.dart:1:1 • x' });
  assert.equal(warn.ok, false, 'warning 必须让 analyze 判红');
  const err = evaluateAnalyze({ output: '  error • compile failed • lib/a.dart:1:1 • x' });
  assert.equal(err.ok, false, 'error 必须让 analyze 判红');
});
