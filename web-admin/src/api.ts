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
  return localStorage.getItem(TOKEN_KEY);
}

export function setToken(t: string | null) {
  if (t) localStorage.setItem(TOKEN_KEY, t);
  else localStorage.removeItem(TOKEN_KEY);
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
    const env = body as ErrorEnvelope | null;
    if (env && typeof env.code === 'number') throw new ApiError(env);
    throw new Error(`请求失败（HTTP ${res.status}）`);
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

export type UserRow = {
  id: string;
  phone_mask: string;
  status: string;
  created_at: string;
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
  login: (username: string, password: string, totp: string) =>
    request<{ token: string; role: string; username: string }>('/admin/v1/login', {
      method: 'POST',
      body: JSON.stringify({ username, password, totp }),
    }),

  overview: () => request<Overview>('/admin/v1/stats/overview'),

  users: () => request<{ items: UserRow[]; total: number }>('/admin/v1/users'),
  banUser: (accountId: string, reason: string) =>
    request<{ ok: boolean }>('/admin/v1/users/ban', {
      method: 'POST',
      body: JSON.stringify({ account_id: accountId, reason }),
    }),

  devices: () => request<{ items: DeviceRow[]; total: number }>('/admin/v1/devices'),

  sessions: (state?: string) =>
    request<{ items: SessionRow[]; total: number }>(`/admin/v1/sessions${state ? `?state=${state}` : ''}`),
  killSession: (sessionId: string, reason: string) =>
    request<{ ok: boolean }>('/admin/v1/sessions/kill', {
      method: 'POST',
      body: JSON.stringify({ session_id: sessionId, reason }),
    }),

  audit: () => request<{ items: AuditRow[]; total: number }>('/admin/v1/audit'),

  config: () => request<ConfigView>('/admin/v1/config'),
  saveConfig: (values: Record<string, string>) =>
    request<{ config: Record<string, string>; audited: boolean }>('/admin/v1/config', {
      method: 'PUT',
      body: JSON.stringify({ values }),
    }),
};
