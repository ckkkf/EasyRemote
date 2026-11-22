// ER-014：错误信封字段在后台 API 层与展示元数据里都不能丢。
import { describe, expect, it } from 'vitest';
import { ApiError, toApiError } from '../src/api';
import { errorMeta } from '../src/errors';

const envelope = {
  code: 100004,
  name: 'AUTH_TOO_MANY_ATTEMPTS',
  message: '登录尝试过多',
  retryable: true,
  trace_id: 'trace-web-1',
};

describe('error envelope', () => {
  it('TestWebApiErrorKeepsEnvelopeFields', () => {
    const err = new ApiError(envelope);
    expect(err.code).toBe(100004);
    expect(err.name).toBe('AUTH_TOO_MANY_ATTEMPTS');
    expect(err.message).toBe('登录尝试过多');
    expect(err.retryable).toBe(true);
    expect(err.traceId).toBe('trace-web-1');
  });

  it('TestWebNonEnvelopeErrorIsStructured', () => {
    const err = toApiError(502, 'bad gateway');
    expect(err.name).toBe('HTTP_ERROR');
    expect(err.code).toBe(502);
    expect(err.retryable).toBe(true);
    expect(err.traceId).toBe('');

    const bad = toApiError(400, null);
    expect(bad.retryable).toBe(false);
  });

  it('errorMeta drives retry button and trace copy', () => {
    const err = toApiError(200, envelope);
    const meta = errorMeta(err, true);
    expect(meta.traceId).toBe('trace-web-1');
    expect(meta.canRetry).toBe(true);

    const noHandler = errorMeta(err, false);
    expect(noHandler.canRetry).toBe(false);

    const notRetryable = new ApiError({ ...envelope, retryable: false });
    expect(errorMeta(notRetryable, true).canRetry).toBe(false);

    const unknown = errorMeta(new Error('boom'), true);
    expect(unknown.name).toBe('UNKNOWN');
    expect(unknown.canRetry).toBe(false);
  });
});
