// ADM-UI-07：会话状态枚举必须与服务端状态机一致（曾经 UI 用 'ended'，筛不到也禁用不住）。
import { describe, expect, it } from 'vitest';
import { SESSION_STATES, isTerminal, stateLabel, stateTone } from '../src/features/sessions/session-states';

describe('session states', () => {
  it('TestSessionStatesMatchServerMachine', () => {
    // server/internal/session/session.go:19-22 只有这四个状态。
    expect([...SESSION_STATES]).toEqual(['requested', 'authorized', 'active', 'closed']);
    expect(SESSION_STATES).not.toContain('ended');
  });

  it('TestTerminalStateIsClosed', () => {
    expect(isTerminal('closed')).toBe(true);
    for (const s of ['requested', 'authorized', 'active']) expect(isTerminal(s)).toBe(false);
  });

  it('TestStateLabelsAndTones', () => {
    expect(stateLabel('closed')).toBe('已结束');
    expect(stateLabel('active')).toBe('进行中');
    expect(stateLabel('weird')).toBe('weird');
    expect(stateTone('active')).toBe('ok');
    expect(stateTone('requested')).toBe('warn');
    expect(stateTone('closed')).toBe('muted');
    expect(stateTone('authorized')).toBe('brand');
  });
});
