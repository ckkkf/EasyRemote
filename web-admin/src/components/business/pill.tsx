// 状态胶囊：全站唯一实现（颜色语义来自 styles.css 的 .pill 变体）。
import type { ReactNode } from 'react';

export type PillTone = 'ok' | 'warn' | 'muted' | 'danger' | 'brand';

export function Pill({ tone, children }: { tone: PillTone; children: ReactNode }) {
  return <span className={'pill ' + tone}>{children}</span>;
}
