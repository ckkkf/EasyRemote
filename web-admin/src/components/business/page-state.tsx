// 页面三态（加载/空/错误）的唯一实现：所有页面共用，禁止再各写一套。
//
// 错误态必须同时给出人话 message、错误码 code/name 与可复制的 trace_id（AGENTS §8.9），
// 可重试性由 errorMeta 依据错误信封的 retryable 决定，不是每个页面自己猜。
import { errorMeta } from '../../errors';
import { Alert, AlertDescription, AlertTitle } from '../ui/alert';
import { Button } from '../ui/button';

export function LoadingState({ text = '加载中…' }: { text?: string }) {
  return (
    <div role="status" aria-live="polite" className="py-9 text-center text-sm text-muted-foreground">
      {text}
    </div>
  );
}

export function EmptyState({ text, hint }: { text: string; hint?: string }) {
  return (
    <div className="py-9 text-center text-sm text-muted-foreground">
      <p>{text}</p>
      {hint ? <p className="mt-1 text-xs">{hint}</p> : null}
    </div>
  );
}

export function ErrorState({
  error,
  onRetry,
  onClose,
}: {
  error: unknown;
  onRetry?: () => void;
  onClose?: () => void;
}) {
  if (!error) return null;
  const meta = errorMeta(error, Boolean(onRetry));
  const copyTrace = () => {
    if (!meta.traceId) return;
    void navigator.clipboard?.writeText(meta.traceId);
  };
  return (
    <Alert variant="destructive">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <AlertTitle>{meta.message}</AlertTitle>
          <AlertDescription className="mt-1 font-mono text-xs">
            {meta.code} {meta.name}
            {meta.traceId ? ' · trace ' + meta.traceId : ''}
          </AlertDescription>
        </div>
        <div className="flex shrink-0 gap-2">
          {meta.traceId ? (
            <Button variant="outline" size="sm" onClick={copyTrace}>
              复制 trace
            </Button>
          ) : null}
          {meta.canRetry && onRetry ? (
            <Button variant="outline" size="sm" onClick={onRetry}>
              重试
            </Button>
          ) : null}
          {onClose ? (
            <Button variant="ghost" size="sm" onClick={onClose}>
              关闭
            </Button>
          ) : null}
        </div>
      </div>
    </Alert>
  );
}
