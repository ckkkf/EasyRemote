// 中断结果语义（ER-812）：关闭 / 传播 / 审计是三个独立结果，不能合并成单一"成功"。
// 抽成纯函数，便于在无法注入传播失败的本地环境里用单测覆盖 partial 分支。
import type { KillSessionResult } from '../../api';

export type KillOutcome = {
  tone: 'ok' | 'partial' | 'error';
  message: string;
  detail?: string;
};

export function describeKillResult(res: KillSessionResult, sessionId: string): KillOutcome {
  if (!res.closed) {
    return { tone: 'error', message: '中断未生效：服务端未关闭会话', detail: res.warning };
  }
  if (!res.propagated) {
    return {
      tone: 'partial',
      message: '会话已关闭，但传播失败：' + (res.warning ?? '请刷新确认双方状态'),
      detail: res.warning,
    };
  }
  if (!res.audited) {
    return { tone: 'partial', message: '会话已关闭，但审计写入失败', detail: res.warning };
  }
  return { tone: 'ok', message: '已中断 ' + sessionId + '（传播与审计均已确认）' };
}
