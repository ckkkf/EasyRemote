// 后台契约测试：能力台账与配置中心必须同源，且不提供任何运行时解锁开关。
import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { CAPABILITIES } from '../src/capability_gen';

const ROOT = resolve(__dirname, '..');

function yamlKeys(): string[] {
  const text = readFileSync(resolve(ROOT, '../docs/spec/capabilities.yaml'), 'utf8');
  const keys: string[] = [];
  for (const line of text.split('\n')) {
    const m = line.match(/^  ([a-z][a-z0-9_]*):$/);
    if (m) keys.push(m[1]);
  }
  return keys;
}

describe('capability ledger', () => {
  it('台账非空、字段完整、成熟度合法', () => {
    expect(CAPABILITIES.length).toBeGreaterThan(0);
    for (const c of CAPABILITIES) {
      expect(c.key).toMatch(/^[a-z][a-z0-9_]*$/);
      expect(c.title.length).toBeGreaterThan(0);
      expect(c.owner).toMatch(/^ER-\d+/);
      expect(c.reason.length).toBeGreaterThan(0);
      expect(['disabled', 'experimental', 'beta', 'stable']).toContain(c.maturity);
    }
  });

  it('与 docs/spec/capabilities.yaml 的 key 一一对应', () => {
    expect(CAPABILITIES.map((c) => c.key)).toEqual(yamlKeys());
  });

  it('配置中心只展示台账，不提供运行时解锁', () => {
    const app = readFileSync(resolve(ROOT, 'src/App.tsx'), 'utf8');
    expect(app).toContain("from './capability_gen'");
    expect(app).toContain('CAPABILITIES.map');
    expect(app).toContain('这里不提供开关');
  });
});
