// ADM-UI-02a：路由表是导航、页面标题、权限守卫与深链的唯一来源。
// 这里守的是"URL 契约"和"权限口径"，不守组件内部实现。
import { describe, expect, it } from 'vitest';
import {
  ADMIN_ROUTES,
  HOME_PATH,
  LOGIN_PATH,
  canAccess,
  childPath,
  normalizePath,
  resolvePath,
  routeById,
  routeByPath,
  visibleRoutes,
  type PageId,
} from '../src/app/routes';

describe('admin route table', () => {
  it('TestAdminRouteTableCoversNav', () => {
    const ids: PageId[] = [
      'dashboard',
      'users',
      'devices',
      'sessions',
      'audit',
      'config',
      'admins',
      'risk',
      'versions',
    ];
    expect(ADMIN_ROUTES.map((r) => r.id)).toEqual(ids);

    const paths = ADMIN_ROUTES.map((r) => r.path);
    expect(new Set(paths).size).toBe(paths.length);
    for (const route of ADMIN_ROUTES) {
      expect(route.path.startsWith('/')).toBe(true);
      expect(route.label.length).toBeGreaterThan(0);
      expect(route.icon.length).toBeGreaterThan(0);
      // 布局路由的子路由必须是相对路径，否则 v6 会当成非法嵌套。
      expect(childPath(route)).toBe(route.path.slice(1));
      expect(routeById(route.id).path).toBe(route.path);
      expect(routeByPath(route.path)?.id).toBe(route.id);
    }
  });

  it('TestRoutePathsMatchDeepLinks', () => {
    // 深链是对外契约：改路径等于改书签与审计里的 URL，必须显式改测试。
    expect(ADMIN_ROUTES.map((r) => r.path)).toEqual([
      '/dashboard',
      '/users',
      '/devices',
      '/sessions',
      '/audit',
      '/config',
      '/admins',
      '/risk',
      '/versions',
    ]);
    expect(HOME_PATH).toBe('/dashboard');
    expect(LOGIN_PATH).toBe('/login');
  });

  it('TestRouteResolution', () => {
    expect(resolvePath('/login', []).kind).toBe('login');
    expect(resolvePath('/', []).kind).toBe('home');
    expect(resolvePath('/dashboard', [])).toEqual({ kind: 'page', route: routeById('dashboard') });
    expect(resolvePath('/users/', []).kind).toBe('page');
    expect(resolvePath('/users?x=1', []).kind).toBe('page');
    expect(resolvePath('/nope', [])).toEqual({ kind: 'notfound', path: '/nope' });
    // 详情子路径归属父页面（/users/:id 的标题与导航高亮都按"用户"）。
    expect(resolvePath('/users/acc-1', [])).toEqual({ kind: 'page', route: routeById('users') });
    expect(resolvePath('/config/x', [])).toEqual({ kind: 'forbidden', route: routeById('config') });
    expect(resolvePath('/config', [])).toEqual({ kind: 'forbidden', route: routeById('config') });
    expect(resolvePath('/config', ['config.read'])).toEqual({ kind: 'page', route: routeById('config') });
  });

  it('TestPermissionGateOnlyConfig', () => {
    // 与现状对齐：除配置中心外，其余页面按登录响应可见，不新增服务端没有的权限动作。
    // A14 与配置中心都要求权限；没有权限时都不可见。
    const blocked = ADMIN_ROUTES.filter((r) => !canAccess(r, []));
    expect(blocked.map((r) => r.id)).toEqual(['config', 'admins', 'risk', 'versions']);
    expect(visibleRoutes([]).map((r) => r.id)).toEqual(['dashboard', 'users', 'devices', 'sessions', 'audit']);
    expect(visibleRoutes(['config.read']).map((r) => r.id)).toContain('config');
  });

  it('TestNormalizePath', () => {
    expect(normalizePath('')).toBe('/');
    expect(normalizePath('/')).toBe('/');
    expect(normalizePath('/users/')).toBe('/users');
    expect(normalizePath('/users#x')).toBe('/users');
  });
});
