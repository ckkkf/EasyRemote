// 会话状态枚举：唯一来源是服务端状态机 server/internal/session/session.go:19-22。
// 曾经 UI 用 'ended' 做筛选和禁用条件，而真实终态是 'closed'，导致筛 'ended' 永远 0 条、
// 已结束会话的中断按钮还能点（ADM-UI-00 基线记录，本卡修复）。
import type { PillTone } from '../../components/business/pill';

export const SESSION_STATES = ['requested', 'authorized', 'active', 'closed'] as const;
export type SessionState = (typeof SESSION_STATES)[number];

export const SESSION_STATE_LABEL: Record<SessionState, string> = {
  requested: '等待授权',
  authorized: '已授权',
  active: '进行中',
  closed: '已结束',
};

export function isTerminal(state: string): boolean {
  return state === 'closed';
}

export function stateTone(state: string): PillTone {
  if (state === 'active') return 'ok';
  if (state === 'requested') return 'warn';
  if (state === 'closed') return 'muted';
  return 'brand';
}

export function stateLabel(state: string): string {
  return SESSION_STATE_LABEL[state as SessionState] ?? state;
}
