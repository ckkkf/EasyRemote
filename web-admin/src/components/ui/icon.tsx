import type { SVGProps } from 'react';

/**
 * 图标集（内联 SVG，无新增依赖）。
 *
 * 为什么不用 lucide-react：仓库当前没有该依赖，而这一轮的目标是"统一视觉"，
 * 不应该顺手引入新包；这些路径来自 lucide 的同名图标（ISC），保持 24×24 网格。
 */
type IconName =
  | 'dashboard'
  | 'users'
  | 'devices'
  | 'sessions'
  | 'audit'
  | 'config'
  | 'shield'
  | 'versions'
  | 'admins'
  | 'refresh'
  | 'search';

const PATHS: Record<IconName, string> = {
  dashboard: 'M3 3h7v9H3zM14 3h7v5h-7zM14 12h7v9h-7zM3 16h7v5H3z',
  users: 'M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2M9 7a4 4 0 1 0 0 8 4 4 0 0 0 0-8zM22 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75',
  devices: 'M3 5h18v10H3zM8 19h8M12 15v4',
  sessions: 'M4 7h16M4 12h10M4 17h7',
  audit: 'M12 8v4l3 2M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18z',
  config: 'M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6zM19.4 15a7.9 7.9 0 0 0 .1-1 7.9 7.9 0 0 0-.1-1l2-1.6-2-3.4-2.4 1a8 8 0 0 0-1.7-1L15 4h-6l-.3 2.6a8 8 0 0 0-1.7 1l-2.4-1-2 3.4L4.6 13a7.9 7.9 0 0 0-.1 1 7.9 7.9 0 0 0 .1 1l-2 1.6 2 3.4 2.4-1a8 8 0 0 0 1.7 1L9 20h6l.3-2.6a8 8 0 0 0 1.7-1l2.4 1 2-3.4z',
  shield: 'M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z',
  versions: 'M12 19V5M5 12l7-7 7 7',
  admins: 'M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM4 21v-1a6 6 0 0 1 6-6h4a6 6 0 0 1 6 6v1',
  refresh: 'M21 12a9 9 0 1 1-3-6.7M21 3v6h-6',
  search: 'M11 19a8 8 0 1 0 0-16 8 8 0 0 0 0 16zM21 21l-4.3-4.3',
};

export function Icon({ name, className, ...props }: { name: IconName } & SVGProps<SVGSVGElement>) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.75"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      className={className}
      {...props}
    >
      <path d={PATHS[name]} />
    </svg>
  );
}
