// 管理端路由表：页面 id、路径、导航文案与所需权限的唯一来源。
//
// 为什么要独立成模块：路由、导航高亮、页面标题、权限守卫必须读同一份表，
// 否则刷新/深链、隐藏入口、服务端 403 三处会各说一套（ADM-UI-02a）。
// 权限口径与当前实现保持一致：只有配置中心需要 config.read，其余页面按登录响应可见。
export type PageId =
  | 'dashboard'
  | 'users'
  | 'devices'
  | 'sessions'
  | 'audit'
  | 'config'
  | 'admins'
  | 'risk'
  | 'versions';

export type AdminRoute = {
  id: PageId;
  path: string;
  label: string;
  icon: string;
  /** 需要的权限动作（来自登录响应的 permissions），缺省表示登录即可见。 */
  permission?: string;
};

export const HOME_PATH = '/dashboard';
export const LOGIN_PATH = '/login';

export const ADMIN_ROUTES: readonly AdminRoute[] = [
  { id: 'dashboard', path: '/dashboard', label: '仪表盘', icon: '▦' },
  { id: 'users', path: '/users', label: '用户', icon: '☰' },
  { id: 'devices', path: '/devices', label: '设备', icon: '▤' },
  { id: 'sessions', path: '/sessions', label: '会话', icon: '⇄' },
  { id: 'audit', path: '/audit', label: '审计日志', icon: '◷' },
  { id: 'config', path: '/config', label: '配置中心', icon: '⚙', permission: 'config.read' },
  // A14：只对超管可见（admin.read），非超管直接看不到入口、深链也会 403。
  { id: 'admins', path: '/admins', label: '管理员与角色', icon: '☗', permission: 'admin.read' },
  // A10：风控中心，风控/运营/超管/只读可见（risk.read），深链同样受守卫。
  { id: 'risk', path: '/risk', label: '风控中心', icon: '⛨', permission: 'risk.read' },
  // A11：版本管理，运营/超管/只读可见（version.read）。
  { id: 'versions', path: '/versions', label: '版本管理', icon: '⬆', permission: 'version.read' },
] as const;

export function routeById(id: PageId): AdminRoute {
  const found = ADMIN_ROUTES.find((r) => r.id === id);
  if (!found) throw new Error('unknown admin route: ' + id);
  return found;
}

/** 布局路由的子路由必须用相对路径；路径仍只来自 ADMIN_ROUTES，避免两处手写。 */
export function childPath(route: AdminRoute): string {
  return route.path.replace(/^\//, '');
}

export function routeByPath(pathname: string): AdminRoute | undefined {
  const clean = normalizePath(pathname);
  return ADMIN_ROUTES.find((r) => r.path === clean);
}

export function normalizePath(pathname: string): string {
  if (!pathname) return '/';
  const cut = pathname.split('?')[0].split('#')[0];
  if (cut.length > 1 && cut.endsWith('/')) return cut.slice(0, -1);
  return cut;
}

export function canAccess(route: AdminRoute, permissions: readonly string[]): boolean {
  return !route.permission || permissions.includes(route.permission);
}

export function visibleRoutes(permissions: readonly string[]): AdminRoute[] {
  return ADMIN_ROUTES.filter((r) => canAccess(r, permissions));
}

export type Resolution =
  | { kind: 'page'; route: AdminRoute }
  | { kind: 'forbidden'; route: AdminRoute }
  | { kind: 'notfound'; path: string }
  | { kind: 'login' }
  | { kind: 'home' };

/** 把当前 URL 解析成渲染决策；App 的标题、守卫与 404 都走这一个函数。 */
export function resolvePath(pathname: string, permissions: readonly string[]): Resolution {
  const clean = normalizePath(pathname);
  if (clean === LOGIN_PATH) return { kind: 'login' };
  if (clean === '/') return { kind: 'home' };
  const route = routeByPath(clean);
  if (!route) {
    // 详情类子路径（/users/:id）归属父页面：标题、权限与导航高亮都跟随父页面。
    const parent = ADMIN_ROUTES.find((r) => clean.startsWith(r.path + '/'));
    if (parent) {
      if (!canAccess(parent, permissions)) return { kind: 'forbidden', route: parent };
      return { kind: 'page', route: parent };
    }
    return { kind: 'notfound', path: clean };
  }
  if (!canAccess(route, permissions)) return { kind: 'forbidden', route };
  return { kind: 'page', route };
}
