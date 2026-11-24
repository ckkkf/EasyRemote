// ADM-UI-02c：对话框必须走 Radix Dialog（焦点陷阱/Esc/aria），不能再留自绘 modal。
import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const SRC = resolve(__dirname, '../src');
const read = (rel: string) => readFileSync(resolve(SRC, rel), 'utf8');

describe('dialog migration', () => {
  it('TestDialogsUseRadixDialog', () => {
    const app = read('App.tsx');
    // 旧的本地 Modal 组件与其 class 必须彻底消失，否则就是两套遮罩层并存。
    expect(app).not.toContain('function Modal');
    expect(app).not.toContain('modal-mask');
    expect(app).not.toContain('modal-actions');
    expect(app).toContain("from './components/business/confirm-dialog'");
    expect(app).toContain('<ConfirmDialog');
    // 详情已迁到独立路由页面（ADM-UI-05a/06a），组件本身仍保留给 A8 会话详情复用。
    const detail = read('components/business/detail-dialog.tsx');
    expect(detail).toContain("from '../ui/dialog'");
    expect(detail).toContain('<Dialog');

    const dialog = read('components/ui/dialog.tsx');
    expect(dialog).toContain('@radix-ui/react-dialog');
    expect(dialog).toContain('DialogPrimitive.Content');

    const css = readFileSync(resolve(__dirname, '../src/styles.css'), 'utf8');
    expect(css).not.toContain('.modal-mask');
    expect(css).not.toContain('.modal-actions');
  });

  it('TestConfirmDialogGatesDangerAction', () => {
    const src = read('components/business/confirm-dialog.tsx');
    // 危险操作走 destructive 变体；禁用态由调用方的业务规则计算，组件不自己猜。
    expect(src).toContain("danger ? 'destructive' : 'default'");
    expect(src).toContain('disabled={busy || confirmDisabled}');
    // Esc/遮罩关闭统一回调 onClose，避免有的弹窗关不掉。
    expect(src).toContain('onOpenChange');
  });
});
