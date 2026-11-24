// ADM-UI-02b：三态组件是页面状态的唯一实现，错误必须带信封字段与可复制 trace。
import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { EmptyState, ErrorState, LoadingState } from '../src/components/business/page-state';
import { ApiError } from '../src/api';

describe('page state components', () => {
  it('TestLoadingStateIsAnnounced', () => {
    const html = renderToStaticMarkup(<LoadingState />);
    expect(html).toContain('role="status"');
    expect(html).toContain('aria-live="polite"');
    expect(html).toContain('加载中…');

    const custom = renderToStaticMarkup(<LoadingState text="读取设备…" />);
    expect(custom).toContain('读取设备…');
  });

  it('TestEmptyStateRendersTextAndHint', () => {
    const html = renderToStaticMarkup(<EmptyState text="没有匹配的用户" />);
    expect(html).toContain('没有匹配的用户');

    const withHint = renderToStaticMarkup(<EmptyState text="暂无会话" hint="会话建立后会出现在这里" />);
    expect(withHint).toContain('会话建立后会出现在这里');
  });

  it('TestErrorStateShowsEnvelopeAndActions', () => {
    const err = new ApiError({
      code: 700007,
      name: 'ADMIN_SESSION_EXPIRED',
      message: '登录已过期',
      retryable: true,
      trace_id: 'trace-err-1',
    });
    const html = renderToStaticMarkup(<ErrorState error={err} onRetry={() => {}} onClose={() => {}} />);
    expect(html).toContain('role="alert"');
    expect(html).toContain('登录已过期');
    expect(html).toContain('700007');
    expect(html).toContain('ADMIN_SESSION_EXPIRED');
    expect(html).toContain('trace-err-1');
    expect(html).toContain('复制 trace');
    expect(html).toContain('重试');
    expect(html).toContain('关闭');
  });

  it('TestErrorStateRespectsRetryability', () => {
    const notRetryable = new ApiError({
      code: 700008,
      name: 'ADMIN_FORBIDDEN',
      message: '没有权限',
      retryable: false,
      trace_id: '',
    });
    const html = renderToStaticMarkup(<ErrorState error={notRetryable} onRetry={() => {}} />);
    // 不可重试的错误不给"重试"按钮，避免把权限问题包装成网络抖动。
    expect(html).not.toContain('重试');
    expect(html).not.toContain('复制 trace');

    // 没有错误时不渲染任何内容。
    expect(renderToStaticMarkup(<ErrorState error={null} />)).toBe('');
  });
});
