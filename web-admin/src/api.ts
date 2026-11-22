// API 客户端：与 server/internal/adminhttp 的契约一一对应。
// 所有写操作失败都会返回结构化错误信封（code/name/message/retryable/trace_id）。

export type ErrorEnvelope = {
  code: number;
  name: string;
  message: string;
  retryable: boolean;
  trace_id: string;
};

export class ApiError extends Error {
  readonly code: number;
  readonly name: string;
  readonly retryable: boolean;
  readonly traceId: string;

  constructor(e: ErrorEnvelope) {
    super(e.message);
    this.code = e.code;
    this.name = e.name;
    this.retryable = e.retryable;
    this.traceId = e.trace_id;
  }
}

const TOKEN_KEY = 'er.admin.token';

export function getToken(): string | null {
  return sessionStorage.getItem(TOKEN_KEY);
}

export type AdminIdentity = {
  username: string;
  role: string;
  permissions: string[];
};

const IDENTITY_KEY = 'er_admin_identity';

export function getIdentity(): AdminIdentity | null {
  const raw = sessionStorage.getItem(IDENTITY_KEY);
  if (!raw) return null;
  try {
    return JSON.parse(raw) as AdminIdentity;
  } catch {
    return null;
  }
}

export function setIdentity(identity: AdminIdentity | null) {
  if (identity) sessionStorage.setItem(IDENTITY_KEY, JSON.stringify(identity));
  else sessionStorage.removeItem(IDENTITY_KEY);
}

export function setToken(t: string | null) {
  if (t) sessionStorage.setItem(TOKEN_KEY, t);
  else sessionStorage.removeItem(TOKEN_KEY);
}

/** 把任意错误响应转成结构化 ApiError（非信封响应也要保留 status/retryable）。 */
export function toApiError(status: number, body: unknown): ApiError {
  const env = body as Partial<ErrorEnvelope> | null;
  if (env && typeof env.code === 'number' && typeof env.name === 'string') {
    return new ApiError(env as ErrorEnvelope);
  }
  return new ApiError({
    code: status,
    name: 'HTTP_ERROR',
    message: `请求失败（HTTP ${status}）`,
    retryable: status >= 500,
    trace_id: '',
  });
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const token = getToken();
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(init.headers as Record<string, string> | undefined),
  };
  if (token) headers.Authorization = `Bearer ${token}`;

  const res = await fetch(path, { ...init, headers });
  const text = await res.text();
  const body = text ? JSON.parse(text) : null;

  if (!res.ok) {
    throw toApiError(res.status, body);
  }
  // 成功信封：{code:0, name:"OK", data, trace_id}。data 之外的字段不属于业务。
  const env = body as { code?: number; data?: unknown } | null;
  if (env && typeof env.code === 'number') {
    if (env.code !== 0) throw new ApiError(body as ErrorEnvelope);
    return env.data as T;
  }
  return body as T;
}

export type Overview = {
  devices_total: number;
  devices_online: number;
  sessions_active: number;
  sessions_pending: number;
  generated_at: string;
};

export type ListQuery = {
  limit?: number;
  offset?: number;
  q?: string;
  sort?: string;
  desc?: boolean;
  status?: string;
  platform?: string;
  action?: string;
  result?: string;
};

function listUrl(path: string, q: ListQuery = {}): string {
  const params = new URLSearchParams();
  if (q.limit) params.set('limit', String(q.limit));
  if (q.offset) params.set('offset', String(q.offset));
  if (q.q) params.set('q', q.q);
  if (q.sort) params.set('sort', q.sort);
  if (q.desc) params.set('desc', '1');
  if (q.status) params.set('status', q.status);
  if (q.platform) params.set('platform', q.platform);
  if (q.action) params.set('action', q.action);
  if (q.result) params.set('result', q.result);
  const qs = params.toString();
  return qs ? path + '?' + qs : path;
}

export type UserRow = {
  id: string;
  phone_mask: string;
  status: string;
  created_at: string;
};

export type KillSessionResult = {
  closed: boolean;
  propagated: boolean;
  audited: boolean;
  warning?: string;
};

export type DeviceRow = {
  id: string;
  device_code: string;
  account_id: string;
  platform: string;
  online: boolean;
  /** 后台接口字段名是 last_seen（不是 last_seen_at）。 */
  last_seen?: string;
  last_seen_at?: string;
};

export type SessionRow = {
  id: string;
  controller_id: string;
  target_id: string;
  state: string;
  reason?: string;
  started_at: string;
  ended_at?: string;
};

/** 配置中心字段（对应 contract.ConfigField，Dart 侧叫 defaultValue）。 */
export type ConfigField = {
  key: string;
  kind: string;
  default: string;
  min?: number;
  max?: number;
  help: string;
};

export type ConfigView = {
  config: Record<string, string>;
  fields: ConfigField[];
};

export type AuditRow = {
  id: string;
  actor: string;
  role: string;
  action: string;
  object: string;
  before?: string;
  after?: string;
  ip: string;
  result: string;
  ts: string;
};

export const api = {
  logout: () => request<{ ok: boolean }>('/admin/v1/logout', { method: 'POST' }),

  login: (username: string, password: string, totp: string) =>
    request<{
      token: string;
      role: string;
      username: string;
      permissions: string[];
      expires_at: string;
    }>('/admin/v1/login', {
      method: 'POST',
      body: JSON.stringify({ username, password, totp }),
    }),

  overview: () => request<Overview>('/admin/v1/stats/overview'),

  users: (q: ListQuery = {}) =>
    request<{ items: UserRow[]; total: number }>(listUrl('/admin/v1/users', q)),
  banUser: (accountId: string, reason: string) =>
    request<{ ok: boolean }>('/admin/v1/users/ban', {
      method: 'POST',
      body: JSON.stringify({ account_id: accountId, reason }),
    }),

  devices: (q: ListQuery = {}) =>
    request<{ items: DeviceRow[]; total: number }>(listUrl('/admin/v1/devices', q)),

  sessions: (state?: string, q: ListQuery = {}) =>
    request<{ items: SessionRow[]; total: number }>(
      listUrl('/admin/v1/sessions', state ? { ...q, status: state } : q),
    ),
  killSession: async (sessionId: string, reason: string) => {
    const ctrl = new AbortController();
    const timer = setTimeout(() => ctrl.abort(), 10_000);
    try {
      return await request<KillSessionResult>('/admin/v1/sessions/kill', {
        method: 'POST',
        body: JSON.stringify({ session_id: sessionId, reason }),
        signal: ctrl.signal,
      });
    } finally {
      clearTimeout(timer);
    }
  },

  audit: (q: ListQuery = {}) =>
    request<{ items: AuditRow[]; total: number }>(listUrl('/admin/v1/audit', q)),

  config: () => request<ConfigView>('/admin/v1/config'),
  saveConfig: (values: Record<string, string>) =>
    request<{ config: Record<string, string>; audited: boolean }>('/admin/v1/config', {
      method: 'PUT',
      body: JSON.stringify({ values }),
    }),
};