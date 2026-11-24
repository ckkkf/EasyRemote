// ADM-UI-10b：中断结果的三种语义（成功 / 部分失败 / 未生效）必须在 UI 层可判定。
// 本地单进程 stack 无法注入传播失败，所以 partial 分支用纯函数单测覆盖。
import { describe, expect, it } from 'vitest';
import { describeKillResult } from '../src/features/sessions/kill-result';

describe('kill result semantics', () => {
  it('TestKillResultOkRequiresClosedPropagatedAudited', () => {
    const ok = describeKillResult({ closed: true, propagated: true, audited: true }, 'ses-1');
    expect(ok.tone).toBe('ok');
    expect(ok.message).toContain('ses-1');
    expect(ok.message).toContain('传播与审计均已确认');
  });

  it('TestKillResultPartialOnPropagationFailure', () => {
    const partial = describeKillResult(
      { closed: true, propagated: false, audited: true, warning: 'peer offline' },
      'ses-2',
    );
    expect(partial.tone).toBe('partial');
    expect(partial.message).toContain('传播失败');
    expect(partial.detail).toBe('peer offline');
    // 不能把部分失败说成成功。
    expect(partial.message).not.toContain('均已确认');
  });

  it('TestKillResultPartialOnAuditFailure', () => {
    const partial = describeKillResult({ closed: true, propagated: true, audited: false }, 'ses-3');
    expect(partial.tone).toBe('partial');
    expect(partial.message).toContain('审计写入失败');
  });

  it('TestKillResultErrorWhenNotClosed', () => {
    const err = describeKillResult({ closed: false, propagated: false, audited: false }, 'ses-4');
    expect(err.tone).toBe('error');
    expect(err.message).toContain('未生效');
  });
});
