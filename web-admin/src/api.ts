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
  /** 至少有一台在线设备的账号数（服务端 presence 判定，ADM-API-02a）。 */
  users_online: number;
  /** 质量指标来自客户端采样（ADM-API-02e）；false 时前端必须显示「数据未提供」。 */
  quality_available: boolean;
  latency_p50_ms: number;
  latency_p95_ms: number;
  relay_percent: number;
  quality_samples: number;
  /** 稳定性（ADM-API-02f1）：窗口内结束会话按原因归一统计。 */
  stability_available: boolean;
  closed_sessions: number;
  abnormal_closes: number;
  abnormal_close_rate: number;
  /** 建连成功率（ADM-API-02f2）：窗口内 active 尝试 / 总尝试。 */
  connect_available: boolean;
  connect_attempts: number;
  connect_success_rate: number;
  /** 链路与帧率（ADM-API-02f4）：只统计客户端上报过的采样。 */
  link_available: boolean;
  p2p_hit_rate: number;
  fps_available: boolean;
  fps_target_rate: number;
  /** 客户端健康（ADM-API-02f5）：崩溃率 = 崩溃/窗口内会话数；重连成功率 = 成功/尝试。 */
  devices_new_available: boolean;
  devices_new_24h: number;
  crash_available: boolean;
  crash_rate: number;
  reconnect_available: boolean;
  reconnect_success_rate: number;
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
  /** 会话状态筛选：服务端读 state（与 status 不同，后者用于账号状态）。 */
  state?: string;
  platform?: string;
  /** 设备在线状态过滤（true/false；undefined 表示全部）。 */
  online?: boolean;
  action?: string;
  result?: string;
  /** 审计过滤：操作人/对象精确匹配，from/to 为 RFC3339。 */
  actor?: string;
  object?: string;
  /** 会话过滤（ADM-API-03f1）：账号 / 设备。 */
  account_id?: string;
  device_id?: string;
  /** 会话筛选（ADM-API-03f2）：结束分类 / 是否异常 / 链路类型。 */
  close_kind?: string;
  abnormal?: boolean;
  link?: string;
  from?: string;
  to?: string;
};

function listUrl(path: string, q: ListQuery = {}): string {
  const params = new URLSearchParams();
  if (q.limit) params.set('limit', String(q.limit));
  if (q.offset) params.set('offset', String(q.offset));
  if (q.q) params.set('q', q.q);
  if (q.sort) params.set('sort', q.sort);
  if (q.desc) params.set('desc', '1');
  if (q.status) params.set('status', q.status);
  if (q.state) params.set('state', q.state);
  if (q.platform) params.set('platform', q.platform);
  if (q.online !== undefined) params.set('online', q.online ? '1' : '0');
  if (q.action) params.set('action', q.action);
  if (q.result) params.set('result', q.result);
  if (q.actor) params.set('actor', q.actor);
  if (q.object) params.set('object', q.object);
  if (q.account_id) params.set('account_id', q.account_id);
  if (q.device_id) params.set('device_id', q.device_id);
  if (q.close_kind) params.set('close_kind', q.close_kind);
  if (q.abnormal !== undefined) params.set('abnormal', q.abnormal ? '1' : '0');
  if (q.link) params.set('link', q.link);
  if (q.from) params.set('from', q.from);
  if (q.to) params.set('to', q.to);
  const qs = params.toString();
  return qs ? path + '?' + qs : path;
}

export type UserRow = {
  id: string;
  /** 昵称；为空表示未设置（没有资料编辑接口）。 */
  nickname: string;
  phone_mask: string;
  status: string;
  /** 已绑定设备数（服务端聚合，ADM-API-03a）。 */
  device_count: number;
  /** 最近登录时间；历史账号可能为 0001-01-01（从未登录）。 */
  last_login_at?: string;
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
  /** 客户端上报的设备元信息；空串表示尚未上报（ADM-API-04a）。 */
  device_name: string;
  os_version: string;
  client_version: string;
  nat_type: string;
  /** 人工风险标记（ADM-API-04c）。 */
  risk_flag: boolean;
  risk_reason: string;
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
  /** 会话级质量摘要（ADM-API-02f7）：无采样时 latency_available=false。 */
  latency_available: boolean;
  avg_latency_ms: number;
  link: string;
};

/** 配置中心字段（对应 contract.ConfigField，Dart 侧叫 defaultValue）。 */
export type ConfigField = {
  /** 分组（画质/连接/限流/开关），ADM-API-05c1。 */
  group: string;
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
  /** 当前生效作用域（"" = 全局），ADM-API-05c1；version 为空表示该平台任意版本（05d）。 */
  platform?: string;
  version?: string;
};

export type ClientVersionRow = {
  id: string;
  platform: string;
  version: string;
  status: string;
  rollout_percent: number;
  force_update: boolean;
  notes: string;
  created_at: string;
  updated_at: string;
};

export type SessionParty = {
  device_id: string;
  device_code: string;
  platform: string;
  os_version: string;
  client_version: string;
  online: boolean;
};

export type LinkSwitch = { ts: string; from: string; to: string };
export type SessionErrorRow = {
  ts: string;
  code: number;
  name: string;
  detail: string;
  source: string;
};
export type TimelineEntry = { ts: string; label: string; detail: string };

export type KillRequestRow = {
  id: string;
  session_id: string;
  reason: string;
  requested_by: string;
  requested_at: string;
  expires_at: string;
  status: string;
  confirmed_by: string;
  confirmed_at?: string;
};

export type BlacklistRow = {
  id: string;
  kind: string;
  value: string;
  note: string;
  expires_at?: string;
  created_at: string;
  created_by: string;
};

export type RiskAlertRow = {
  id: string;
  kind: string;
  subject: string;
  detail: string;
  ts: string;
};

export type TrendPoint = {
  bucket_start: string;
  sessions_started: number;
  connect_attempts: number;
  connect_active: number;
};

export type AdminRow = {
  id: string;
  username: string;
  role: string;
  status: string;
  mfa_enabled: boolean;
};

export type DeviceQualityDay = {
  date: string;
  avg_latency_ms: number;
  avg_loss_permille: number;
  relay_percent: number;
  samples: number;
};

export type QualitySample = {
  ts: string;
  latency_ms: number;
  loss_permille: number;
  fps: number;
  tier: string;
  relay: boolean;
};

export type ConfigHistoryEntry = {
  id: string;
  action: string;
  actor: string;
  /** before/after 是「本次改动键」的 JSON 字符串。 */
  before: string;
  after: string;
  result: string;
  ts: string;
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

  trends: (range: '24h' | '7d' | '30d' = '24h', bucket: 'hour' | 'day' = 'hour') =>
    request<{ range: string; bucket: string; points: TrendPoint[] }>(
      '/admin/v1/stats/trends?range=' + range + '&bucket=' + bucket,
    ),
  overview: (range: '24h' | '7d' | '30d' = '24h') =>
    request<Overview>('/admin/v1/stats/overview?range=' + range),

  users: (q: ListQuery = {}) =>
    request<{ items: UserRow[]; total: number }>(listUrl('/admin/v1/users', q)),
  banUser: (accountId: string, reason: string) =>
    request<{ ok: boolean }>('/admin/v1/users/ban', {
      method: 'POST',
      body: JSON.stringify({ account_id: accountId, reason }),
    }),

  devices: (q: ListQuery = {}) =>
    request<{ items: DeviceRow[]; total: number }>(listUrl('/admin/v1/devices', q)),
  forceOffline: (deviceId: string, reason: string) =>
    request<{ ok: boolean }>('/admin/v1/devices/force-offline', {
      method: 'POST',
      body: JSON.stringify({ device_id: deviceId, reason }),
    }),
  flagDeviceRisk: (deviceId: string, reason: string) =>
    request<{ ok: boolean }>('/admin/v1/devices/flag-risk', {
      method: 'POST',
      body: JSON.stringify({ device_id: deviceId, reason }),
    }),
  clearDeviceRisk: (deviceId: string, reason: string) =>
    request<{ ok: boolean }>('/admin/v1/devices/clear-risk', {
      method: 'POST',
      body: JSON.stringify({ device_id: deviceId, reason }),
    }),
  unbindDevice: (deviceId: string, reason: string) =>
    request<{ ok: boolean }>('/admin/v1/devices/unbind', {
      method: 'POST',
      body: JSON.stringify({ device_id: deviceId, reason }),
    }),

  // 服务端读的是 query 参数 state（adminhttp handler），不是 status——用 status 会让筛选静默失效。
  sessions: (state?: string, q: ListQuery = {}) =>
    request<{ items: SessionRow[]; total: number }>(
      listUrl('/admin/v1/sessions', state ? { ...q, state } : q),
    ),
  deviceQuality: (deviceId: string, days = 30) =>
    request<{ device_id: string; available: boolean; days: DeviceQualityDay[] }>(
      '/admin/v1/devices/quality?device_id=' + encodeURIComponent(deviceId) + '&days=' + days,
    ),
  killRequest: (sessionId: string, reason: string) =>
    request<KillRequestRow>('/admin/v1/sessions/kill-request', {
      method: 'POST',
      body: JSON.stringify({ session_id: sessionId, reason }),
    }),
  killConfirm: (requestId: string) =>
    request<{ closed: boolean; propagated: boolean; audited: boolean; warning?: string }>(
      '/admin/v1/sessions/kill-confirm',
      { method: 'POST', body: JSON.stringify({ request_id: requestId }) },
    ),
  killRequests: (status = 'pending') =>
    request<{ items: KillRequestRow[]; total: number }>(
      '/admin/v1/sessions/kill-requests?status=' + encodeURIComponent(status),
    ),
  sessionDetail: (sessionId: string) =>
    request<{
      controller: SessionParty;
      target: SessionParty;
      link_switches: LinkSwitch[];
      timeline: TimelineEntry[];
      error_events: SessionErrorRow[];
    }>('/admin/v1/sessions/detail?session_id=' + encodeURIComponent(sessionId)),
  sessionQuality: (sessionId: string) =>
    request<{ session_id: string; available: boolean; samples: QualitySample[] }>(
      '/admin/v1/sessions/quality?session_id=' + encodeURIComponent(sessionId),
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

  admins: (q: ListQuery = {}) =>
    request<{ items: AdminRow[]; total: number }>(listUrl('/admin/v1/admins', q)),
  updateAdmin: (adminId: string, patch: { role?: string; status?: string; reason: string }) =>
    request<{ ok: boolean }>('/admin/v1/admins/update', {
      method: 'POST',
      body: JSON.stringify({ admin_id: adminId, ...patch }),
    }),
  versions: (q: ListQuery = {}) =>
    request<{ items: ClientVersionRow[]; total: number }>(listUrl('/admin/v1/versions', q)),
  publishVersion: (req: {
    platform: string;
    version: string;
    status: string;
    rollout_percent: number;
    force_update: boolean;
    notes: string;
  }) => request<ClientVersionRow>('/admin/v1/versions', { method: 'POST', body: JSON.stringify(req) }),
  retireVersion: (versionId: string, reason: string) =>
    request<{ ok: boolean }>('/admin/v1/versions/retire', {
      method: 'POST',
      body: JSON.stringify({ version_id: versionId, reason }),
    }),
  riskBlacklist: (q: ListQuery = {}) =>
    request<{ items: BlacklistRow[]; total: number }>(listUrl('/admin/v1/risk/blacklist', q)),
  addBlacklist: (req: { kind: string; value: string; note: string; ttl_hours?: number }) =>
    request<BlacklistRow>('/admin/v1/risk/blacklist', { method: 'POST', body: JSON.stringify(req) }),
  removeBlacklist: (id: string, reason: string) =>
    request<{ ok: boolean }>('/admin/v1/risk/blacklist/remove', {
      method: 'POST',
      body: JSON.stringify({ id, reason }),
    }),
  riskAlerts: (q: ListQuery = {}) =>
    request<{ items: RiskAlertRow[]; total: number }>(listUrl('/admin/v1/risk/alerts', q)),
  audit: (q: ListQuery = {}) =>
    request<{ items: AuditRow[]; total: number }>(listUrl('/admin/v1/audit', q)),
  exportUsers: (q: ListQuery = {}) =>
    request<{ csv: string; rows: number; truncated: boolean }>(listUrl('/admin/v1/users/export', q)),
  unbanUser: (accountId: string, reason: string) =>
    request<{ ok: boolean }>('/admin/v1/users/unban', {
      method: 'POST',
      body: JSON.stringify({ account_id: accountId, reason }),
    }),
  kickUser: (accountId: string, reason: string) =>
    request<{ ok: boolean }>('/admin/v1/users/kick', {
      method: 'POST',
      body: JSON.stringify({ account_id: accountId, reason }),
    }),
  userSessions: (accountId: string, limit = 20) =>
    request<{ items: SessionRow[]; total: number }>(
      '/admin/v1/users/sessions?account_id=' + encodeURIComponent(accountId) + '&limit=' + limit,
    ),
  userNote: (accountId: string) =>
    request<{ account_id: string; note: string; updated_by: string; updated_at?: string }>(
      '/admin/v1/users/note?account_id=' + encodeURIComponent(accountId),
    ),
  setUserNote: (accountId: string, note: string) =>
    request<{ ok: boolean }>('/admin/v1/users/note', {
      method: 'POST',
      body: JSON.stringify({ account_id: accountId, note }),
    }),
  auditActions: () => request<{ actions: string[] }>('/admin/v1/audit/actions'),
  exportAudit: (q: ListQuery = {}) =>
    request<{ csv: string; rows: number; truncated: boolean }>(listUrl('/admin/v1/audit/export', q)),

  config: (platform = '', version = '') => {
    const params = new URLSearchParams();
    if (platform) params.set('platform', platform);
    if (version) params.set('version', version);
    const qs = params.toString();
    return request<ConfigView>('/admin/v1/config' + (qs ? '?' + qs : ''));
  },
  configHistory: (limit = 20) =>
    request<{ items: ConfigHistoryEntry[] }>('/admin/v1/config/history?limit=' + limit),
  rollbackConfig: (key: string, value: string) =>
    request<{ config: Record<string, string>; audited: boolean }>('/admin/v1/config/rollback', {
      method: 'POST',
      body: JSON.stringify({ key, value }),
    }),
  saveConfig: (values: Record<string, string>, platform = '', version = '') =>
    request<{ config: Record<string, string>; audited: boolean }>('/admin/v1/config', {
      method: 'PUT',
      body: JSON.stringify({ values, platform, version }),
    }),
};