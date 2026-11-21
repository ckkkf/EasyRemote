import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { api, ApiError, getToken, setToken } from './api';
import type { AuditRow, ConfigField, ConfigView, DeviceRow, Overview, SessionRow, UserRow } from './api';

type Page = 'dashboard' | 'users' | 'devices' | 'sessions' | 'audit' | 'config';

const PAGES: { id: Page; label: string; icon: string }[] = [
  { id: 'dashboard', label: '仪表盘', icon: '▦' },
  { id: 'users', label: '用户', icon: '☰' },
  { id: 'devices', label: '设备', icon: '▤' },
  { id: 'sessions', label: '会话', icon: '⇄' },
  { id: 'audit', label: '审计日志', icon: '◷' },
  { id: 'config', label: '配置中心', icon: '⚙' },
];

const PAGE_SIZE = 10;

/* ---------- 基础组件 ---------- */

function ErrorBanner({ error, onClose }: { error: unknown; onClose?: () => void }) {
  if (!error) return null;
  const body =
    error instanceof ApiError ? (
      <>
        {error.message}
        <span className="err-meta">
          {error.code} {error.name}
          {error.traceId ? ' · trace ' + error.traceId : ''}
        </span>
      </>
    ) : (
      String(error)
    );
  return (
    <div className="banner error">
      <span>{body}</span>
      {onClose && (
        <button className="link" onClick={onClose}>
          关闭
        </button>
      )}
    </div>
  );
}

function InfoBanner({ text, onClose }: { text: string; onClose: () => void }) {
  return (
    <div className="banner ok">
      <span>{text}</span>
      <button className="link" onClick={onClose}>
        关闭
      </button>
    </div>
  );
}

function Loading({ text = '加载中…' }: { text?: string }) {
  return <div className="placeholder">{text}</div>;
}

function Empty({ text }: { text: string }) {
  return <div className="placeholder">{text}</div>;
}

function Pill({ tone, children }: { tone: 'ok' | 'warn' | 'muted' | 'danger' | 'brand'; children: React.ReactNode }) {
  return <span className={'pill ' + tone}>{children}</span>;
}

function Card({ title, subtitle, actions, children }: {
  title?: string;
  subtitle?: string;
  actions?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <section className="card">
      {(title || actions) && (
        <header className="card-head">
          <div>
            {title && <h3>{title}</h3>}
            {subtitle && <p>{subtitle}</p>}
          </div>
          {actions && <div className="card-actions">{actions}</div>}
        </header>
      )}
      {children}
    </section>
  );
}

function Pagination({ page, total, onChange }: { page: number; total: number; onChange: (p: number) => void }) {
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  if (pages <= 1) return <div className="pager muted">共 {total} 条</div>;
  return (
    <div className="pager">
      <span className="muted">共 {total} 条 · 第 {page} / {pages} 页</span>
      <div className="pager-btns">
        <button className="btn ghost sm" disabled={page <= 1} onClick={() => onChange(page - 1)}>
          上一页
        </button>
        <button className="btn ghost sm" disabled={page >= pages} onClick={() => onChange(page + 1)}>
          下一页
        </button>
      </div>
    </div>
  );
}

function Modal({ title, children, onClose }: { title: string; children: React.ReactNode; onClose: () => void }) {
  return (
    <div className="modal-mask" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <h3>{title}</h3>
        {children}
      </div>
    </div>
  );
}

function relTime(iso: string): string {
  if (!iso || iso.startsWith('0001')) return '—';
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return iso;
  const diff = Date.now() - t;
  if (diff < 60_000) return '刚刚';
  if (diff < 3_600_000) return Math.floor(diff / 60_000) + ' 分钟前';
  if (diff < 86_400_000) return Math.floor(diff / 3_600_000) + ' 小时前';
  return Math.floor(diff / 86_400_000) + ' 天前';
}

function fmtCode(code: string): string {
  const clean = code.replace(/\s/g, '');
  if (clean.length !== 9) return code;
  return clean.slice(0, 3) + ' ' + clean.slice(3, 6) + ' ' + clean.slice(6);
}

function fmtTime(iso: string): string {
  if (!iso || iso.startsWith('0001')) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString('zh-CN', { hour12: false });
}

function duration(s: SessionRow): string {
  if (!s.started_at) return '—';
  const start = new Date(s.started_at).getTime();
  const end = s.ended_at && !s.ended_at.startsWith('0001') ? new Date(s.ended_at).getTime() : Date.now();
  const sec = Math.max(0, Math.floor((end - start) / 1000));
  if (sec < 60) return sec + ' 秒';
  if (sec < 3600) return Math.floor(sec / 60) + ' 分 ' + (sec % 60) + ' 秒';
  return Math.floor(sec / 3600) + ' 小时 ' + Math.floor((sec % 3600) / 60) + ' 分';
}

/* ---------- 登录 ---------- */

function Login({ onDone }: { onDone: () => void }) {
  const [username, setUsername] = useState('root');
  const [password, setPassword] = useState('');
  const [totp, setTotp] = useState('');
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const res = await api.login(username, password, totp);
      setToken(res.token);
      onDone();
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="login">
      <aside className="login-brand">
        <div className="logo">ER</div>
        <h1>EasyRemote</h1>
        <p>连得稳，看得清。</p>
        <ul>
          <li>设备与会话总览，异常一眼看到</li>
          <li>封禁、中断、审计都在这里闭环</li>
          <li>所有操作写入不可篡改的审计日志</li>
        </ul>
        <span className="login-ver">admin console · v1.0</span>
      </aside>
      <main className="login-form-wrap">
        <form className="login-form" onSubmit={submit}>
          <h2>登录控制台</h2>
          <p className="muted">仅限授权人员访问，所有操作均写入审计</p>
          <ErrorBanner error={error} />
          <label>账号</label>
          <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" />
          <label>密码</label>
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
          />
          <label>二次验证码</label>
          <input
            inputMode="numeric"
            maxLength={6}
            value={totp}
            onChange={(e) => setTotp(e.target.value)}
            placeholder="6 位数字"
          />
          <button className="btn primary block" disabled={busy}>
            {busy ? '登录中…' : '登录'}
          </button>
          <p className="hint">开发环境：root / dev-pass；服务端设 ER_ADMIN_TOTP_OFF=1 时验证码留空即可</p>
        </form>
      </main>
    </div>
  );
}

/* ---------- 仪表盘 ---------- */

function Dashboard({ onOpen, refreshKey }: { onOpen: (p: Page) => void; refreshKey: number }) {
  const [overview, setOverview] = useState<Overview | null>(null);
  const [devices, setDevices] = useState<DeviceRow[]>([]);
  const [sessions, setSessions] = useState<SessionRow[]>([]);
  const [audit, setAudit] = useState<AuditRow[]>([]);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [auto, setAuto] = useState(true);
  const [updatedAt, setUpdatedAt] = useState<string>('');

  const load = useCallback(async () => {
    try {
      const [ov, dv, ss, au] = await Promise.all([api.overview(), api.devices(), api.sessions(), api.audit()]);
      setOverview(ov);
      setDevices(dv.items);
      setSessions(ss.items);
      setAudit(au.items);
      setUpdatedAt(new Date().toLocaleTimeString('zh-CN', { hour12: false }));
      setError(null);
    } catch (err) {
      setError(err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load, refreshKey]);

  useEffect(() => {
    if (!auto) return;
    const t = window.setInterval(() => void load(), 30_000);
    return () => window.clearInterval(t);
  }, [auto, load]);

  const platforms = useMemo(() => {
    const map = new Map<string, number>();
    for (const d of devices) map.set(d.platform || 'unknown', (map.get(d.platform || 'unknown') ?? 0) + 1);
    return [...map.entries()].sort((a, b) => b[1] - a[1]);
  }, [devices]);

  const onlineRate = devices.length === 0 ? 0 : Math.round(((overview?.devices_online ?? 0) / devices.length) * 100);

  if (loading) return <Loading />;

  return (
    <div className="stack">
      <ErrorBanner error={error} />
      <div className="metrics">
        <button className="metric" onClick={() => onOpen('devices')}>
          <span>设备总数</span>
          <strong>{overview?.devices_total ?? '—'}</strong>
          <em>在线 {overview?.devices_online ?? '—'}</em>
        </button>
        <button className="metric" onClick={() => onOpen('devices')}>
          <span>在线设备</span>
          <strong>{overview?.devices_online ?? '—'}</strong>
          <em>在线率 {onlineRate}%</em>
        </button>
        <button className="metric" onClick={() => onOpen('sessions')}>
          <span>进行中会话</span>
          <strong>{overview?.sessions_active ?? '—'}</strong>
          <em>已授权连接</em>
        </button>
        <button className="metric warn" onClick={() => onOpen('sessions')}>
          <span>待授权</span>
          <strong>{overview?.sessions_pending ?? '—'}</strong>
          <em>等待被控端同意</em>
        </button>
      </div>

      <div className="grid-2">
        <Card title="在线率" subtitle={'设备 ' + devices.length + ' 台 · 在线 ' + (overview?.devices_online ?? 0) + ' 台'}>
          <div className="bar">
            <div className="bar-fill" style={{ width: onlineRate + '%' }} />
          </div>
          <div className="bar-legend">
            <span>在线 {onlineRate}%</span>
            <span className="muted">离线 {100 - onlineRate}%</span>
          </div>
        </Card>
        <Card title="平台分布" subtitle="按设备平台聚合">
          {platforms.length === 0 ? (
            <Empty text="暂无设备" />
          ) : (
            <div className="dist">
              {platforms.map(([name, count]) => (
                <div className="dist-row" key={name}>
                  <span className="dist-name">{name}</span>
                  <div className="dist-bar">
                    <div style={{ width: Math.round((count / devices.length) * 100) + '%' }} />
                  </div>
                  <span className="dist-count">{count}</span>
                </div>
              ))}
            </div>
          )}
        </Card>
      </div>

      <div className="grid-2">
        <Card
          title="最近会话"
          subtitle={'更新于 ' + (updatedAt || '—')}
          actions={
            <>
              <label className="switch">
                <input type="checkbox" checked={auto} onChange={(e) => setAuto(e.target.checked)} /> 自动刷新
              </label>
              <button className="btn ghost sm" onClick={() => void load()}>
                刷新
              </button>
            </>
          }
        >
          {sessions.length === 0 ? (
            <Empty text="暂无会话" />
          ) : (
            <table className="tbl">
              <thead>
                <tr>
                  <th>会话</th>
                  <th>状态</th>
                  <th>时长</th>
                  <th>开始时间</th>
                </tr>
              </thead>
              <tbody>
                {sessions.slice(0, 8).map((s) => (
                  <tr key={s.id}>
                    <td className="mono">{s.id}</td>
                    <td>
                      <Pill tone={s.state === 'active' ? 'ok' : s.state === 'requested' ? 'warn' : 'muted'}>{s.state}</Pill>
                    </td>
                    <td>{duration(s)}</td>
                    <td className="muted">{fmtTime(s.started_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Card>
        <Card title="最近审计" subtitle="登录、封禁、中断等敏感操作">
          {audit.length === 0 ? (
            <Empty text="暂无记录" />
          ) : (
            <table className="tbl">
              <thead>
                <tr>
                  <th>动作</th>
                  <th>操作人</th>
                  <th>结果</th>
                  <th>时间</th>
                </tr>
              </thead>
              <tbody>
                {audit.slice(0, 8).map((a) => (
                  <tr key={a.id}>
                    <td>
                      <span className="mono">{a.action}</span>
                    </td>
                    <td>{a.actor}</td>
                    <td>
                      <Pill tone={a.result === 'ok' ? 'ok' : 'danger'}>{a.result}</Pill>
                    </td>
                    <td className="muted">{relTime(a.ts)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Card>
      </div>
    </div>
  );
}

/* ---------- 用户 ---------- */

function Users({ notice }: { notice: (t: string) => void }) {
  const [rows, setRows] = useState<UserRow[]>([]);
  const [query, setQuery] = useState('');
  const [status, setStatus] = useState('all');
  const [page, setPage] = useState(1);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [target, setTarget] = useState<UserRow | null>(null);
  const [detail, setDetail] = useState<UserRow | null>(null);
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await api.users();
      setRows(res.items);
      setError(null);
    } catch (err) {
      setError(err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const filtered = useMemo(() => {
    const q = query.trim();
    return rows.filter((u) => {
      if (status !== 'all' && u.status !== status) return false;
      if (!q) return true;
      return u.id.includes(q) || u.phone_mask.includes(q);
    });
  }, [rows, query, status]);

  const paged = filtered.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE);

  async function ban() {
    if (!target) return;
    if (reason.trim().length < 4) return;
    setBusy(true);
    try {
      await api.banUser(target.id, reason.trim());
      notice('已封禁 ' + target.id);
      setTarget(null);
      setReason('');
      await load();
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="stack">
      <ErrorBanner error={error} />
      <Card
        title={'用户 (' + rows.length + ')'}
        subtitle={'今日新增 ' + rows.filter((u) => Date.now() - new Date(u.created_at).getTime() < 864e5).length + ' · 受限 ' + rows.filter((u) => u.status !== 'active').length}
        actions={
          <>
            <input
              className="search"
              placeholder="搜索账号或手机号掩码"
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
                setPage(1);
              }}
            />
            <select
              value={status}
              onChange={(e) => {
                setStatus(e.target.value);
                setPage(1);
              }}
            >
              <option value="all">全部状态</option>
              <option value="active">active</option>
              <option value="banned">banned</option>
            </select>
          </>
        }
      >
        {loading ? (
          <Loading />
        ) : paged.length === 0 ? (
          <Empty text="没有匹配的用户" />
        ) : (
          <table className="tbl">
            <thead>
              <tr>
                <th>账号 ID</th>
                <th>手机号</th>
                <th>状态</th>
                <th>注册时间</th>
                <th className="right">操作</th>
              </tr>
            </thead>
            <tbody>
              {paged.map((u) => (
                <tr key={u.id} className="clickable" onClick={() => setDetail(u)} title="点击查看用户详情">
                  <td className="mono">{u.id}</td>
                  <td>{u.phone_mask}</td>
                  <td>
                    <Pill tone={u.status === 'active' ? 'ok' : 'danger'}>{u.status}</Pill>
                  </td>
                  <td className="muted">{fmtTime(u.created_at)}</td>
                  <td className="right">
                    <button
                      className="btn danger sm"
                      disabled={u.status !== 'active'}
                      onClick={(e) => {
                        e.stopPropagation();
                        setTarget(u);
                      }}
                    >
                      封禁
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        <Pagination page={page} total={filtered.length} onChange={setPage} />
      </Card>

      {detail && <UserDetail user={detail} onClose={() => setDetail(null)} />}

      {target && (
        <Modal title={'封禁 ' + target.id} onClose={() => setTarget(null)}>
          <p className="muted">封禁后该账号的所有设备将无法建立会话，操作会写入审计。</p>
          <label>原因（至少 4 个字）</label>
          <textarea value={reason} onChange={(e) => setReason(e.target.value)} rows={3} />
          <div className="modal-actions">
            <button className="btn ghost" onClick={() => setTarget(null)}>
              取消
            </button>
            <button className="btn danger" disabled={busy || reason.trim().length < 4} onClick={() => void ban()}>
              {busy ? '提交中…' : '确认封禁'}
            </button>
          </div>
        </Modal>
      )}
    </div>
  );
}

/* ---------- 设备 ---------- */

function Devices({ notice }: { notice: (t: string) => void }) {
  const [rows, setRows] = useState<DeviceRow[]>([]);
  const [query, setQuery] = useState('');
  const [online, setOnline] = useState('all');
  const [platform, setPlatform] = useState('all');
  const [page, setPage] = useState(1);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [detail, setDetail] = useState<DeviceRow | null>(null);

  const load = useCallback(async () => {
    try {
      const res = await api.devices();
      setRows(res.items);
      setError(null);
    } catch (err) {
      setError(err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const platforms = useMemo(() => [...new Set(rows.map((d) => d.platform))], [rows]);

  const filtered = useMemo(() => {
    const q = query.trim();
    return rows.filter((d) => {
      if (online === 'online' && !d.online) return false;
      if (online === 'offline' && d.online) return false;
      if (platform !== 'all' && d.platform !== platform) return false;
      if (!q) return true;
      return d.device_code.includes(q) || d.account_id.includes(q) || d.platform.includes(q);
    });
  }, [rows, query, online, platform]);

  const paged = filtered.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE);

  return (
    <div className="stack">
      <ErrorBanner error={error} />
      <Card
        title={'设备 (' + rows.length + ')'}
        subtitle={'在线 ' + rows.filter((d) => d.online).length + ' 台 · 平台 ' + platforms.length + ' 种'}
        actions={
          <>
            <input
              className="search"
              placeholder="搜索设备码 / 账号 / 平台"
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
                setPage(1);
              }}
            />
            <select value={online} onChange={(e) => { setOnline(e.target.value); setPage(1); }}>
              <option value="all">全部状态</option>
              <option value="online">在线</option>
              <option value="offline">离线</option>
            </select>
            <select value={platform} onChange={(e) => { setPlatform(e.target.value); setPage(1); }}>
              <option value="all">全部平台</option>
              {platforms.map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </select>
            <button className="btn ghost sm" onClick={() => void load()}>
              刷新
            </button>
          </>
        }
      >
        {loading ? (
          <Loading />
        ) : paged.length === 0 ? (
          <Empty text="没有匹配的设备" />
        ) : (
          <table className="tbl">
            <thead>
              <tr>
                <th>设备码</th>
                <th>归属账号</th>
                <th>平台</th>
                <th>在线</th>
                <th>最后在线</th>
                <th className="right">操作</th>
              </tr>
            </thead>
            <tbody>
              {paged.map((d) => (
                <tr key={d.id || d.device_code} className="clickable" onClick={() => setDetail(d)} title="点击查看设备详情">
                  <td className="mono big">{fmtCode(d.device_code)}</td>
                  <td className="mono">{d.account_id}</td>
                  <td>{d.platform}</td>
                  <td>
                    <Pill tone={d.online ? 'ok' : 'muted'}>{d.online ? '在线' : '离线'}</Pill>
                  </td>
                  <td className="muted">{relTime(d.last_seen || d.last_seen_at || '')}</td>
                  <td className="right">
                    <button
                      className="btn ghost sm"
                      onClick={(e) => {
                        e.stopPropagation();
                        void navigator.clipboard?.writeText(d.device_code.replace(/\s/g, ''));
                        notice('设备码已复制');
                      }}
                    >
                      复制设备码
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        <Pagination page={page} total={filtered.length} onChange={setPage} />
      </Card>

      {detail && <DeviceDetail device={detail} onClose={() => setDetail(null)} />}
    </div>
  );
}

/* ---------- 会话 ---------- */

function Sessions({ notice }: { notice: (t: string) => void }) {
  const [rows, setRows] = useState<SessionRow[]>([]);
  const [state, setState] = useState('all');
  const [page, setPage] = useState(1);
  const [auto, setAuto] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [target, setTarget] = useState<SessionRow | null>(null);
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await api.sessions(state === 'all' ? undefined : state);
      setRows(res.items);
      setError(null);
    } catch (err) {
      setError(err);
    } finally {
      setLoading(false);
    }
  }, [state]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (!auto) return;
    const t = window.setInterval(() => void load(), 15_000);
    return () => window.clearInterval(t);
  }, [auto, load]);

  const paged = rows.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE);

  async function kill() {
    if (!target) return;
    if (reason.trim().length < 2) return;
    setBusy(true);
    try {
      await api.killSession(target.id, reason.trim());
      notice('已中断 ' + target.id);
      setTarget(null);
      setReason('');
      await load();
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="stack">
      <ErrorBanner error={error} />
      <Card
        title={'会话 (' + rows.length + ')'}
        subtitle="每 15 秒自动刷新 · 中断会立即生效并写入审计"
        actions={
          <>
            <select value={state} onChange={(e) => { setState(e.target.value); setPage(1); }}>
              <option value="all">全部状态</option>
              <option value="requested">requested</option>
              <option value="authorized">authorized</option>
              <option value="active">active</option>
              <option value="ended">ended</option>
            </select>
            <label className="switch">
              <input type="checkbox" checked={auto} onChange={(e) => setAuto(e.target.checked)} /> 自动刷新
            </label>
            <button className="btn ghost sm" onClick={() => void load()}>
              刷新
            </button>
          </>
        }
      >
        {loading ? (
          <Loading />
        ) : paged.length === 0 ? (
          <Empty text="暂无会话" />
        ) : (
          <table className="tbl">
            <thead>
              <tr>
                <th>会话 ID</th>
                <th>主控</th>
                <th>被控</th>
                <th>状态</th>
                <th>时长</th>
                <th>开始时间</th>
                <th className="right">操作</th>
              </tr>
            </thead>
            <tbody>
              {paged.map((s) => (
                <tr key={s.id}>
                  <td className="mono">{s.id}</td>
                  <td className="mono">{s.controller_id}</td>
                  <td className="mono">{s.target_id}</td>
                  <td>
                    <Pill
                      tone={s.state === 'active' ? 'ok' : s.state === 'requested' ? 'warn' : s.state === 'ended' ? 'muted' : 'brand'}
                    >
                      {s.state}
                    </Pill>
                  </td>
                  <td>{duration(s)}</td>
                  <td className="muted">{fmtTime(s.started_at)}</td>
                  <td className="right">
                    <button
                      className="btn danger sm"
                      disabled={s.state === 'ended'}
                      onClick={() => setTarget(s)}
                    >
                      中断
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        <Pagination page={page} total={rows.length} onChange={setPage} />
      </Card>

      {target && (
        <Modal title={'中断 ' + target.id} onClose={() => setTarget(null)}>
          <p className="muted">中断后双方会立即收到结束事件，操作会写入审计。</p>
          <label>原因</label>
          <input value={reason} onChange={(e) => setReason(e.target.value)} placeholder="例如：用户投诉、异常流量" />
          <div className="modal-actions">
            <button className="btn ghost" onClick={() => setTarget(null)}>
              取消
            </button>
            <button className="btn danger" disabled={busy || reason.trim().length < 2} onClick={() => void kill()}>
              {busy ? '提交中…' : '确认中断'}
            </button>
          </div>
        </Modal>
      )}
    </div>
  );
}

/* ---------- 审计 ---------- */

function Audit() {
  const [rows, setRows] = useState<AuditRow[]>([]);
  const [query, setQuery] = useState('');
  const [action, setAction] = useState('all');
  const [result, setResult] = useState('all');
  const [page, setPage] = useState(1);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    try {
      const res = await api.audit();
      setRows(res.items);
      setError(null);
    } catch (err) {
      setError(err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const actions = useMemo(() => [...new Set(rows.map((r) => r.action))], [rows]);

  const filtered = useMemo(() => {
    const q = query.trim();
    return rows.filter((r) => {
      if (action !== 'all' && r.action !== action) return false;
      if (result !== 'all' && r.result !== result) return false;
      if (!q) return true;
      return r.actor.includes(q) || r.object.includes(q) || r.ip.includes(q);
    });
  }, [rows, query, action, result]);

  const paged = filtered.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE);

  return (
    <div className="stack">
      <ErrorBanner error={error} />
      <Card
        title={'审计日志 (' + rows.length + ')'}
        subtitle="append-only · 记录操作人、对象、IP 与结果"
        actions={
          <>
            <input
              className="search"
              placeholder="搜索操作人 / 对象 / IP"
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
                setPage(1);
              }}
            />
            <select value={action} onChange={(e) => { setAction(e.target.value); setPage(1); }}>
              <option value="all">全部动作</option>
              {actions.map((a) => (
                <option key={a} value={a}>
                  {a}
                </option>
              ))}
            </select>
            <select value={result} onChange={(e) => { setResult(e.target.value); setPage(1); }}>
              <option value="all">全部结果</option>
              <option value="ok">ok</option>
              <option value="denied">denied</option>
            </select>
            <button className="btn ghost sm" onClick={() => void load()}>
              刷新
            </button>
          </>
        }
      >
        {loading ? (
          <Loading />
        ) : paged.length === 0 ? (
          <Empty text="没有匹配的审计记录" />
        ) : (
          <table className="tbl">
            <thead>
              <tr>
                <th>时间</th>
                <th>操作人</th>
                <th>动作</th>
                <th>对象</th>
                <th>结果</th>
                <th>IP</th>
                <th>变更</th>
              </tr>
            </thead>
            <tbody>
              {paged.map((a) => (
                <tr key={a.id}>
                  <td className="muted nowrap">{fmtTime(a.ts)}</td>
                  <td>{a.actor}</td>
                  <td className="mono">{a.action}</td>
                  <td className="mono">{a.object}</td>
                  <td>
                    <Pill tone={a.result === 'ok' ? 'ok' : 'danger'}>{a.result}</Pill>
                  </td>
                  <td className="mono muted">{a.ip}</td>
                  <td className="diff">
                    {a.before || a.after ? (
                      <>
                        {a.before && <span className="before">{a.before}</span>}
                        {a.before && a.after && <span className="arrow">→</span>}
                        {a.after && <span className="after">{a.after}</span>}
                      </>
                    ) : (
                      <span className="muted">—</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        <Pagination page={page} total={filtered.length} onChange={setPage} />
      </Card>
    </div>
  );
}

/* ---------- 配置中心（P1-1） ---------- */

function ConfigCenter({ notice }: { notice: (t: string) => void }) {
  const [view, setView] = useState<ConfigView | null>(null);
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState('');

  const load = useCallback(async () => {
    try {
      const v = await api.config();
      setView(v);
      setDraft(v.config);
      setError(null);
    } catch (err) {
      setError(err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const fields: ConfigField[] = view?.fields ?? [];
  const changed = fields.filter((f) => (draft[f.key] ?? '') !== (view?.config[f.key] ?? ''));
  const invalid = fields.filter((f) => {
    const raw = (draft[f.key] ?? '').trim();
    if (raw === '') return true;
    if (f.kind === 'int' || f.kind === 'int64') {
      const n = Number(raw);
      if (!Number.isFinite(n)) return true;
      if (f.min !== undefined && n < f.min) return true;
      if (f.max !== undefined && n > f.max) return true;
    }
    if (f.kind === 'bool' && raw !== 'true' && raw !== 'false') return true;
    return false;
  });

  async function save() {
    if (changed.length === 0 || invalid.length > 0) return;
    setBusy(true);
    setError(null);
    setSaved('');
    try {
      const values: Record<string, string> = {};
      for (const f of changed) values[f.key] = (draft[f.key] ?? '').trim();
      const res = await api.saveConfig(values);
      setSaved('已保存 ' + changed.length + ' 项' + (res.audited ? '，变更已写入审计（before / after）' : ''));
      notice('配置已保存：' + changed.map((f) => f.key).join('、'));
      await load();
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="stack">
      <ErrorBanner error={error} onClose={() => setError(null)} />
      {saved && <div className="banner ok"><span>{saved}</span></div>}
      <Card
        title="配置中心"
        subtitle="超级管理员可改；保存即下发给客户端（GET /v1/config 白名单键），每次写操作都进审计"
        actions={
          <>
            <button className="btn ghost sm" onClick={() => void load()} disabled={busy}>
              重新加载
            </button>
            <button
              className="btn ghost sm"
              onClick={() => view && setDraft(view.config)}
              disabled={busy || changed.length === 0}
            >
              撤销修改
            </button>
            <button className="btn sm" onClick={() => void save()} disabled={busy || changed.length === 0 || invalid.length > 0}>
              {busy ? '保存中…' : '保存 ' + (changed.length > 0 ? changed.length + ' 项' : '')}
            </button>
          </>
        }
      >
        {loading ? (
          <Loading />
        ) : fields.length === 0 ? (
          <Empty text="服务端没有返回可配置项" />
        ) : (
          <table className="tbl">
            <thead>
              <tr>
                <th>配置项</th>
                <th>当前值</th>
                <th>默认 / 范围</th>
                <th>说明</th>
              </tr>
            </thead>
            <tbody>
              {fields.map((f) => {
                const raw = draft[f.key] ?? '';
                const bad =
                  raw.trim() === '' ||
                  ((f.kind === 'int' || f.kind === 'int64') && !Number.isFinite(Number(raw))) ||
                  ((f.kind === 'int' || f.kind === 'int64') &&
                    ((f.min !== undefined && Number(raw) < f.min) || (f.max !== undefined && Number(raw) > f.max)));
                return (
                  <tr key={f.key}>
                    <td className="mono nowrap">{f.key}</td>
                    <td>
                      {f.kind === 'bool' ? (
                        <select value={raw} onChange={(e) => setDraft({ ...draft, [f.key]: e.target.value })}>
                          <option value="true">true</option>
                          <option value="false">false</option>
                        </select>
                      ) : (
                        <input
                          className={'cfg-input' + (bad ? ' bad' : '')}
                          value={raw}
                          inputMode={f.kind === 'int' || f.kind === 'int64' ? 'numeric' : 'text'}
                          onChange={(e) => setDraft({ ...draft, [f.key]: e.target.value })}
                        />
                      )}
                    </td>
                    <td className="muted nowrap">
                      {f.default}
                      {f.min !== undefined || f.max !== undefined ? ' · ' + (f.min ?? '—') + ' ~ ' + (f.max ?? '—') : ''}
                    </td>
                    <td className="muted">{f.help}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
        {invalid.length > 0 && (
          <p className="muted" style={{ marginTop: 10 }}>
            以下配置项取值不合法：{invalid.map((f) => f.key).join('、')}
          </p>
        )}
      </Card>
    </div>
  );
}

/* ---------- 详情（P1-2） ---------- */

function DeviceSessions({ deviceId }: { deviceId: string }) {
  const [rows, setRows] = useState<SessionRow[] | null>(null);
  const [error, setError] = useState<unknown>(null);

  useEffect(() => {
    let alive = true;
    api
      .sessions()
      .then((r) => {
        if (!alive) return;
        setRows(r.items.filter((s) => s.controller_id === deviceId || s.target_id === deviceId).slice(0, 10));
      })
      .catch((err) => {
        if (alive) setError(err);
      });
    return () => {
      alive = false;
    };
  }, [deviceId]);

  if (error) return <ErrorBanner error={error} />;
  if (!rows) return <Loading text="读取最近会话…" />;
  if (rows.length === 0) return <p className="muted">这台设备还没有会话记录。</p>;
  return (
    <table className="tbl">
      <thead>
        <tr>
          <th>会话</th>
          <th>状态</th>
          <th>发起 → 目标</th>
          <th>开始</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((s) => (
          <tr key={s.id}>
            <td className="mono">{s.id}</td>
            <td>
              <Pill tone={s.state === 'active' ? 'ok' : s.state === 'ended' ? 'muted' : 'warn'}>{s.state}</Pill>
            </td>
            <td className="mono">{s.controller_id.slice(0, 10)} → {s.target_id.slice(0, 10)}</td>
            <td className="muted">{fmtTime(s.started_at)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function DeviceDetail({ device, onClose }: { device: DeviceRow; onClose: () => void }) {
  const online = device.online;
  return (
    <Modal title={'设备详情 ' + fmtCode(device.device_code)} onClose={onClose}>
      <div className="kv">
        <span>设备码</span>
        <strong className="mono big">{fmtCode(device.device_code)}</strong>
        <span>归属账号</span>
        <strong className="mono">{device.account_id}</strong>
        <span>平台</span>
        <strong>{device.platform}</strong>
        <span>在线</span>
        <strong>
          <Pill tone={online ? 'ok' : 'muted'}>{online ? '在线' : '离线'}</Pill>
        </strong>
        <span>最后在线</span>
        <strong>{relTime(device.last_seen || device.last_seen_at || '')}</strong>
      </div>
      <h4 className="detail-h">最近 10 条会话</h4>
      <DeviceSessions deviceId={device.id} />
      <div className="modal-actions">
        <button className="btn ghost" onClick={onClose}>
          关闭
        </button>
      </div>
    </Modal>
  );
}

function UserDetail({ user, onClose }: { user: UserRow; onClose: () => void }) {
  const [devices, setDevices] = useState<DeviceRow[] | null>(null);
  const [sessions, setSessions] = useState<SessionRow[] | null>(null);
  const [error, setError] = useState<unknown>(null);

  useEffect(() => {
    let alive = true;
    Promise.all([api.devices(), api.sessions()])
      .then(([d, s]) => {
        if (!alive) return;
        const mine = d.items.filter((x) => x.account_id === user.id);
        const ids = new Set(mine.map((x) => x.id));
        setDevices(mine);
        setSessions(s.items.filter((x) => ids.has(x.controller_id) || ids.has(x.target_id)).slice(0, 10));
      })
      .catch((err) => {
        if (alive) setError(err);
      });
    return () => {
      alive = false;
    };
  }, [user.id]);

  return (
    <Modal title={'用户详情 ' + user.phone_mask} onClose={onClose}>
      <div className="kv">
        <span>账号 ID</span>
        <strong className="mono">{user.id}</strong>
        <span>状态</span>
        <strong>
          <Pill tone={user.status === 'active' ? 'ok' : 'danger'}>{user.status}</Pill>
        </strong>
        <span>注册时间</span>
        <strong>{fmtTime(user.created_at)}</strong>
        <span>设备数</span>
        <strong>{devices ? devices.length : '—'}</strong>
      </div>
      <h4 className="detail-h">该账号的设备</h4>
      {error ? (
        <ErrorBanner error={error} />
      ) : !devices ? (
        <Loading text="读取设备…" />
      ) : devices.length === 0 ? (
        <p className="muted">这个账号还没有绑定设备。</p>
      ) : (
        <table className="tbl">
          <thead>
            <tr>
              <th>设备码</th>
              <th>平台</th>
              <th>在线</th>
              <th>最后在线</th>
            </tr>
          </thead>
          <tbody>
            {devices.map((d) => (
              <tr key={d.id || d.device_code}>
                <td className="mono big">{fmtCode(d.device_code)}</td>
                <td>{d.platform}</td>
                <td>
                  <Pill tone={d.online ? 'ok' : 'muted'}>{d.online ? '在线' : '离线'}</Pill>
                </td>
                <td className="muted">{relTime(d.last_seen || d.last_seen_at || '')}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <h4 className="detail-h">最近会话</h4>
      {!sessions ? (
        <Loading text="读取会话…" />
      ) : sessions.length === 0 ? (
        <p className="muted">还没有会话记录。</p>
      ) : (
        <table className="tbl">
          <thead>
            <tr>
              <th>会话</th>
              <th>状态</th>
              <th>开始</th>
            </tr>
          </thead>
          <tbody>
            {sessions.map((s) => (
              <tr key={s.id}>
                <td className="mono">{s.id}</td>
                <td>
                  <Pill tone={s.state === 'active' ? 'ok' : s.state === 'ended' ? 'muted' : 'warn'}>{s.state}</Pill>
                </td>
                <td className="muted">{fmtTime(s.started_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <div className="modal-actions">
        <button className="btn ghost" onClick={onClose}>
          关闭
        </button>
      </div>
    </Modal>
  );
}

/* ---------- 主框架 ---------- */

export default function App() {
  const [authed, setAuthed] = useState<boolean>(!!getToken());
  const [page, setPage] = useState<Page>('dashboard');
  const [noticeText, setNoticeText] = useState('');
  const [refreshKey, setRefreshKey] = useState(0);
  const timer = useRef<number | null>(null);

  const notice = useCallback((t: string) => {
    setNoticeText(t);
    if (timer.current) window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setNoticeText(''), 4000);
  }, []);

  if (!authed) {
    return <Login onDone={() => setAuthed(true)} />;
  }

  const current = PAGES.find((p) => p.id === page)!;

  return (
    <div className="shell">
      <aside className="side">
        <div className="brand">
          <div className="logo">ER</div>
          <div>
            <strong>EasyRemote</strong>
            <span>管理后台</span>
          </div>
        </div>
        <nav>
          {PAGES.map((p) => (
            <button key={p.id} className={'nav' + (p.id === page ? ' active' : '')} onClick={() => setPage(p.id)}>
              <span className="nav-ico">{p.icon}</span>
              {p.label}
            </button>
          ))}
        </nav>
        <div className="side-foot">
          <div className="who">
            <span className="avatar">R</span>
            <div>
              <strong>root</strong>
              <span>超级管理员</span>
            </div>
          </div>
          <button
            className="link"
            onClick={() => {
              setToken(null);
              setAuthed(false);
            }}
          >
            退出登录
          </button>
        </div>
      </aside>

      <main className="main">
        <header className="topbar">
          <div>
            <h2>{current.label}</h2>
            <p>只读视角的后台，所有写操作都会进审计</p>
          </div>
          <div className="top-actions">
            <button className="btn ghost sm" onClick={() => setRefreshKey((k) => k + 1)}>
              刷新数据
            </button>
          </div>
        </header>
        {noticeText && <InfoBanner text={noticeText} onClose={() => setNoticeText('')} />}
        {page === 'dashboard' && <Dashboard onOpen={setPage} refreshKey={refreshKey} />}
        {page === 'users' && <Users notice={notice} />}
        {page === 'devices' && <Devices notice={notice} />}
        {page === 'sessions' && <Sessions notice={notice} />}
        {page === 'audit' && <Audit />}
        {page === 'config' && <ConfigCenter notice={notice} />}
      </main>
    </div>
  );
}
