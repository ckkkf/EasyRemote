import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

// ER-811：后台 UI 的 CSP 回归。meta 不能覆盖所有场景，但缺了就是明显退化。
describe('admin CSP', () => {
  it('TestAdminIndexHasRestrictiveCsp', () => {
    const html = readFileSync(join(process.cwd(), 'index.html'), 'utf8');
    expect(html).toContain('http-equiv="Content-Security-Policy"');
    expect(html).toContain("script-src 'self'");
    expect(html).toContain("frame-ancestors 'none'");
    expect(html).not.toContain("script-src 'unsafe-eval'");
  });
});
