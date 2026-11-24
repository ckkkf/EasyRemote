// 强制中断原因：类别是 UI 侧固定选项（PRD §6.3.4 要求"填原因"，A9 要求"下拉 + 自定义"），
// 服务端只校验非空并写审计，因此这里只负责把类别与补充说明拼成可审计的一句话。
export const KILL_REASON_CATEGORIES = [
  { value: 'user_report', label: '用户投诉' },
  { value: 'abnormal_traffic', label: '异常流量' },
  { value: 'security', label: '安全应急' },
  { value: 'maintenance', label: '运维处理' },
  { value: 'other', label: '其他' },
] as const;

export type KillReasonCategory = (typeof KILL_REASON_CATEGORIES)[number]['value'];

export function categoryLabel(value: string): string {
  return KILL_REASON_CATEGORIES.find((c) => c.value === value)?.label ?? value;
}

/** 拼出送审计的原因：类别 + 可选补充说明。 */
export function composeKillReason(category: string, detail: string): string {
  const label = categoryLabel(category).trim();
  const extra = detail.trim();
  if (!extra) return label;
  return label + '：' + extra;
}

/** 服务端要求原因非空；UI 要求先选类别，拼出的原因至少 2 个字。 */
export function isKillReasonValid(category: string, detail: string): boolean {
  if (!category) return false;
  return composeKillReason(category, detail).length >= 2;
}
