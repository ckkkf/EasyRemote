// ADM-UI-09a：A10 风控 / A11 版本 / A14 管理员目前没有任何后端接口。
// 这三页必须"明确未开放"，不能出现假入口；一旦有人偷偷加路由或导航，这里会红。
import { describe, expect, it } from 'vitest';
import { existsSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { ADMIN_ROUTES } from '../src/app/routes';

const ROOT = resolve(__dirname, '../..');
const read = (rel: string) => readFileSync(resolve(ROOT, rel), 'utf8');

describe('admin scope', () => {
  it('TestAdminRoutesMatchImplementedPages', () => {
    // A14（/admins）、A10（/risk）、A11（/versions）均已有真实接口。
    expect(ADMIN_ROUTES.map((r) => r.id)).toEqual([
      'dashboard',
      'users',
      'devices',
      'sessions',
      'audit',
      'config',
      'admins',
      'risk',
      'versions',
    ]);
    // 未实现页面不得出现：目前 A1-A14 已全部有页面或明确不做的能力。
    const paths = ADMIN_ROUTES.map((r) => r.path).join(' ');
    expect(paths).toContain('/versions');
  });

  it('TestNoFakeAdminNavEntries', () => {
    const app = read('web-admin/src/App.tsx');
    // 导航只来自路由表；A14/A10/A11 都必须受权限保护（无权限看不到入口）。
    expect(ADMIN_ROUTES.find((r) => r.id === 'admins')?.permission).toBe('admin.read');
    expect(ADMIN_ROUTES.find((r) => r.id === 'risk')?.permission).toBe('risk.read');
    expect(ADMIN_ROUTES.find((r) => r.id === 'versions')?.permission).toBe('version.read');
    // 导航项只来自路由表，不允许再写死一份 PAGES。
    expect(app).toContain('visibleRoutes(identity.permissions)');
    expect(app).not.toContain('const PAGES');
  });

  it('TestContractHasNoRiskVersionAdminEndpoints', () => {
    const contract = read('docs/spec/contract.yaml');
    const adminPaths = [...contract.matchAll(/path: (\/admin\/v1\/[^\s]*)/g)].map((m) => m[1]).sort();
    // 白名单式：新增 admin 端点必须同步这里（ADM-API-04b 加了两个设备动作）。
    expect(adminPaths).toEqual([
      '/admin/v1/admins',
      '/admin/v1/admins/update',
      '/admin/v1/audit',
      '/admin/v1/audit/actions',
      '/admin/v1/audit/export',
      '/admin/v1/config',
      '/admin/v1/config',
      '/admin/v1/config/history',
      '/admin/v1/config/rollback',
      '/admin/v1/devices',
      '/admin/v1/devices/clear-risk',
      '/admin/v1/devices/flag-risk',
      '/admin/v1/devices/force-offline',
      '/admin/v1/devices/quality',
      '/admin/v1/devices/unbind',
      '/admin/v1/login',
      '/admin/v1/logout',
      '/admin/v1/risk/alerts',
      '/admin/v1/risk/blacklist',
      '/admin/v1/risk/blacklist',
      '/admin/v1/risk/blacklist/remove',
      '/admin/v1/sessions',
      '/admin/v1/sessions/detail',
      '/admin/v1/sessions/kill',
      '/admin/v1/sessions/kill-confirm',
      '/admin/v1/sessions/kill-request',
      '/admin/v1/sessions/kill-requests',
      '/admin/v1/sessions/quality',
      '/admin/v1/stats/overview',
      '/admin/v1/stats/trends',
      '/admin/v1/users',
      '/admin/v1/users/ban',
      '/admin/v1/users/export',
      '/admin/v1/users/kick',
      '/admin/v1/users/note',
      '/admin/v1/users/note',
      '/admin/v1/users/sessions',
      '/admin/v1/users/unban',
      '/admin/v1/versions',
      '/admin/v1/versions',
      '/admin/v1/versions/retire',
    ]);
    // /admins（06a）、/versions（06b）、/risk（06c）已落地；/roles 与 /blacklist 仍未提供。
    for (const forbidden of ['/roles', '/blacklist']) {
      expect(contract).not.toContain('/admin/v1' + forbidden);
    }
  });

  it('TestUnopenedPagesDocumented', () => {
    const card = resolve(ROOT, 'docs/impl/tasks/ADM-API-06-risk-version-admin-contract.md');
    const audit = resolve(ROOT, 'docs/impl/evidence/ADM-UI-09/capability-audit.md');
    expect(existsSync(card)).toBe(true);
    expect(existsSync(audit)).toBe(true);
    const cardText = readFileSync(card, 'utf8');
    for (const page of ['A10', 'A11', 'A14']) expect(cardText).toContain(page);
    expect(readFileSync(audit, 'utf8')).toContain('未开放');
  });
});
