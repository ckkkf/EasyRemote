// 配置中心 diff 与校验：保存前必须能看清"改了什么、为什么不能存"。
// 校验规则只依赖契约给的 kind/min/max，不猜业务语义。
import type { ConfigField } from '../../api';

export type ConfigChange = { key: string; from: string; to: string };

/** 只返回真正变化的项（from !== to），顺序跟随字段定义。 */
export function changedFields(
  fields: ConfigField[],
  draft: Record<string, string>,
  current: Record<string, string>,
): ConfigChange[] {
  const out: ConfigChange[] = [];
  for (const f of fields) {
    const from = current[f.key] ?? '';
    const to = draft[f.key] ?? '';
    if (from !== to) out.push({ key: f.key, from, to });
  }
  return out;
}

/** 不合法的字段：空值、整数越界/非数字、布尔非 true/false。 */
export function invalidFields(fields: ConfigField[], draft: Record<string, string>): ConfigField[] {
  return fields.filter((f) => {
    const raw = (draft[f.key] ?? '').trim();
    if (raw === '') return true;
    if (f.kind === 'int' || f.kind === 'int64') {
      const n = Number(raw);
      if (!Number.isFinite(n)) return true;
      if (f.min !== undefined && n < f.min) return true;
      if (f.max !== undefined && n > f.max) return true;
    }
    if (f.kind === 'bool' && raw !== 'true' && raw !== 'false') return true;
    return false;
  });
}

/** 保存按钮的唯一判定：有写权限、非提交中、有变更、无非法项。 */
export function canSave(opts: {
  canWrite: boolean;
  busy: boolean;
  changes: ConfigChange[];
  invalid: ConfigField[];
}): boolean {
  return opts.canWrite && !opts.busy && opts.changes.length > 0 && opts.invalid.length === 0;
}
