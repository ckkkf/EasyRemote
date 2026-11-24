// ADM-UI-07：中断原因 = 类别 + 可选说明，拼出的字符串进审计。
import { describe, expect, it } from 'vitest';
import {
  KILL_REASON_CATEGORIES,
  categoryLabel,
  composeKillReason,
  isKillReasonValid,
} from '../src/features/sessions/kill-reasons';

describe('kill reasons', () => {
  it('TestKillReasonCategories', () => {
    expect(KILL_REASON_CATEGORIES.length).toBeGreaterThanOrEqual(3);
    expect(KILL_REASON_CATEGORIES.map((c) => c.label)).toContain('用户投诉');
    expect(categoryLabel('user_report')).toBe('用户投诉');
    expect(categoryLabel('unknown')).toBe('unknown');
  });

  it('TestComposeKillReason', () => {
    expect(composeKillReason('user_report', '')).toBe('用户投诉');
    expect(composeKillReason('security', '  异常登录  ')).toBe('安全应急：异常登录');
  });

  it('TestKillReasonValidation', () => {
    // 服务端要求原因非空：没选类别就不允许提交。
    expect(isKillReasonValid('', '')).toBe(false);
    expect(isKillReasonValid('', '有说明但没类别')).toBe(false);
    expect(isKillReasonValid('other', '')).toBe(true);
    expect(isKillReasonValid('maintenance', '例行维护')).toBe(true);
  });
});
