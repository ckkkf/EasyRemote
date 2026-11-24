import type { ReactNode } from 'react';

import { Button } from '../ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../ui/dialog';

/**
 * 二次确认对话框：封禁/中断这类不可撤销操作统一走这里。
 * 焦点陷阱、Esc 关闭、aria-modal 由 Radix Dialog 提供；
 * 确认按钮的可用性由调用方按业务规则计算（例如原因最少字数）。
 */
export function ConfirmDialog({
  open,
  title,
  description,
  children,
  confirmLabel = '确认',
  cancelLabel = '取消',
  danger = false,
  busy = false,
  confirmDisabled = false,
  onConfirm,
  onClose,
}: {
  open: boolean;
  title: string;
  description?: string;
  children?: ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  danger?: boolean;
  busy?: boolean;
  confirmDisabled?: boolean;
  onConfirm: () => void;
  onClose: () => void;
}) {
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          {description ? <DialogDescription>{description}</DialogDescription> : null}
        </DialogHeader>
        {children}
        <DialogFooter>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            {cancelLabel}
          </Button>
          <Button
            variant={danger ? 'destructive' : 'default'}
            onClick={onConfirm}
            disabled={busy || confirmDisabled}
          >
            {busy ? '提交中…' : confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
