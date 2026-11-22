// 错误展示元数据：把 ApiError 与普通异常统一成 UI 可直接消费的结构。
import { ApiError } from './api';

export type ErrorMeta = {
  message: string;
  code: string;
  name: string;
  traceId: string;
  retryable: boolean;
  canRetry: boolean;
};

export function errorMeta(error: unknown, hasRetry = false): ErrorMeta {
  if (error instanceof ApiError) {
    return {
      message: error.message,
      code: String(error.code),
      name: error.name,
      traceId: error.traceId,
      retryable: error.retryable,
      canRetry: error.retryable && hasRetry,
    };
  }
  return {
    message: String(error),
    code: '',
    name: 'UNKNOWN',
    traceId: '',
    retryable: false,
    canRetry: false,
  };
}
