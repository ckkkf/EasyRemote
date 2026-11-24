// 审计筛选：服务端支持 action/actor/object 精确匹配与 from/to 时间范围，
// 合法动作集合由 GET /admin/v1/audit/actions 提供（ADM-API-05a）。
export function normalizeActionFilter(raw: string): string | undefined {
  const v = raw.trim();
  return v === '' ? undefined : v;
}
