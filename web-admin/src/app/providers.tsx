// 应用级 provider：TanStack Query（缓存/轮询/取消）+ 权限上下文。
//
// 本卡只接线，不改任何页面的请求时机；页面仍按原有 useEffect 拉数据，
// 后续卡再逐页迁到 useQuery（ADM-UI-02b/04）。
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createContext, useContext, type ReactNode } from 'react';

const PermissionContext = createContext<readonly string[]>([]);

export function useCan(action: string): boolean {
  return useContext(PermissionContext).includes(action);
}

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // 后台数据要"看到就是最新"：窗口聚焦即重取，但不在错误时无限重试。
      refetchOnWindowFocus: true,
      retry: 1,
      staleTime: 5_000,
    },
  },
});

export function AppProviders({ permissions, children }: { permissions: readonly string[]; children: ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>
      <PermissionContext.Provider value={permissions}>{children}</PermissionContext.Provider>
    </QueryClientProvider>
  );
}
