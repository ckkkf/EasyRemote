// ADM-UI-01a：技术栈收口断言（React 18 冻结值 + 登录页接线证明）。
import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const ROOT = resolve(__dirname, '..');

function readJson<T>(rel: string): T {
  return JSON.parse(readFileSync(resolve(ROOT, rel), 'utf8')) as T;
}

function major(version: string): string {
  return version.replace(/^[^0-9]*/, '').split('.')[0];
}

describe('web-admin stack conformance', () => {
  it('TestWebAdminReact18', () => {
    const pkg = readJson<{ dependencies: Record<string, string>; devDependencies: Record<string, string> }>(
      'package.json',
    );
    // PRD §6.4 冻结 React 18：主版本必须是 18，不能漂到 19。
    expect(major(pkg.dependencies.react)).toBe('18');
    expect(major(pkg.dependencies['react-dom'])).toBe('18');
    expect(major(pkg.devDependencies['@types/react'])).toBe('18');
    expect(major(pkg.devDependencies['@types/react-dom'])).toBe('18');

    // 锁文件必须真的解析到 18.x，而不是只改了 package.json 的 range。
    const lock = readJson<{ packages: Record<string, { version?: string }> }>('package-lock.json');
    expect(lock.packages['node_modules/react']?.version).toMatch(/^18\./);
    expect(lock.packages['node_modules/react-dom']?.version).toMatch(/^18\./);
    expect(lock.packages['node_modules/@types/react']?.version).toMatch(/^18\./);
  });

  it('TestLoginUsesShadcnButton', () => {
    const app = readFileSync(resolve(ROOT, 'src/App.tsx'), 'utf8');
    // 接线证明：登录页提交按钮来自仓库内 primitive，而不是旧的手写 .btn。
    expect(app).toMatch(/import \{[^}]*\bButton\b[^}]*\} from '\.\/components\/ui\/button'/);
    expect(app).toContain('<Button');
    expect(app).not.toContain('className="btn primary block"');
  });
});
