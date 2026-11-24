import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Navigate, NavLink, Outlet, Route, Routes, useLocation, useNavigate } from 'react-router-dom';
import { api, getIdentity, setIdentity, setToken } from './api';
import type {
  AdminIdentity,
  AdminRow,
  BlacklistRow,
  ClientVersionRow,
  KillRequestRow,
  RiskAlertRow,
  TrendPoint,
  AuditRow,
  ConfigField,
  ConfigHistoryEntry,
  ConfigView,
  DeviceRow,
  Overview,
  SessionRow,
  UserRow,
} from './api';
import { AppProviders, useCan } from './app/providers';
import {
  HOME_PATH,
  LOGIN_PATH,
  canAccess,
  childPath,
  resolvePath,
  routeById,
  visibleRoutes,
  type PageId,
} from './app/routes';
import { ConfirmDialog } from './components/business/confirm-dialog';
import { DataTable } from './components/business/data-table';
import { EmptyState, ErrorState, LoadingState } from './components/business/page-state';
import { Pill } from './components/business/pill';
import { DeviceDetailPage } from './features/devices/device-detail-page';
import { normalizeActionFilter } from './features/audit/filters';
import { canSave, changedFields, invalidFields } from './features/config/diff';
import { KILL_REASON_CATEGORIES, composeKillReason, isKillReasonValid } from './features/sessions/kill-reasons';
import { describeKillResult } from './features/sessions/kill-result';
import { SessionDetailPage } from './features/sessions/session-detail-page';
import { SESSION_STATES, isTerminal, stateLabel, stateTone } from './features/sessions/session-states';
import { UserDetailPage } from './features/users/user-detail-page';
import {
  POLL_INTERVAL_MS,
  UNAVAILABLE_HINT,
  buildMetricGroups,
  formatUpdatedAt,
  isStale,
  onlineRate,
  trendPath,
} from './features/dashboard/metrics';
import { CAPABILITIES } from './capability_gen';
import { Alert, AlertDescription, AlertTitle } from './components/ui/alert';
import { Button, buttonVariants } from './components/ui/button';
import { cn } from './lib/utils';
import { Icon } from './components/ui/icon';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from './components/ui/table';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from './components/ui/select';
import { Switch } from './components/ui/switch';

// 导航图标映射：路由表仍是唯一来源（id），图标只是展示层（ADM-UI-11a）。
const NAV_ICONS: Record<PageId, 'dashboard' | 'users' | 'devices' | 'sessions' | 'audit' | 'config' | 'admins' | 'shield' | 'versions'> = {
  dashboard: 'dashboard',
  users: 'users',
  devices: 'devices',
  sessions: 'sessions',
  audit: 'audit',
  config: 'config',
  admins: 'admins',
  risk: 'shield',
  versions: 'versions',
};
// 业务里已有一个带标题/操作区的 Card（布局组合），shadcn 的原子 Card 用 Ui 前缀区分，
// 避免同名冲突，也避免把"卡片布局"和"卡片原子"混成一个概念。
import {
  Card as UiCard,
  CardContent as UiCardContent,
  CardDescription as UiCardDescription,
  CardHeader as UiCardHeader,
  CardTitle as UiCardTitle,
} from './components/ui/card';
import { Input } from './components/ui/input';
import { Label } from './components/ui/label';
import { Textarea } from './components/ui/textarea';
import { errorMeta } from './errors';

const ROLE_LABEL: Record<string, string> = {
  super: '超级管理员',
  ops: '运维',
  cs: '客服',
  risk: '风控',
  readonly: '只读',
};

// Radix Select 不接受空字符串 value：空语义（全部/全局/未选择）用哨兵值表示。
const SENTINEL = '__none__';

const PAGE_SIZE = 10;

/* ---------- 基础组件 ---------- */

function InfoBanner({ text, onClose }: { text: string; onClose: () => void }) {
  return (
    <div className="banner ok">
      <span>{text}</span>
      <button className={cn(buttonVariants({ variant: 'link' }), 'h-auto min-h-6 p-0 text-xs')} onClick={onClose}>
        关闭
      </button>
    </div>
  );
}

function Card({ title, subtitle, actions, children }: {
  title?: string;
  subtitle?: string;
  actions?: React.ReactNode;
  children: React.ReactNode;
}) {
  // 用 shadcn Card 的排版（ADM-UI-11a）：标题/描述/操作区统一走设计令牌，
  // 不再依赖 styles.css 里的 .card / .card-head 手写规则。
  return (
    <UiCard data-card="1">
      {(title || actions) && (
        <UiCardHeader className="flex flex-row items-start justify-between gap-3 space-y-0">
          <div className="space-y-1">
            {title && <UiCardTitle className="text-base">{title}</UiCardTitle>}
            {subtitle && <UiCardDescription>{subtitle}</UiCardDescription>}
          </div>
          {actions && <div className="flex flex-wrap items-center justify-end gap-2">{actions}</div>}
        </UiCardHeader>
      )}
      <UiCardContent className={title || actions ? '' : 'pt-6'}>{children}</UiCardContent>
    </UiCard>
  );
}

function Pagination({ page, total, onChange }: { page: number; total: number; onChange: (p: number) => void }) {
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  if (pages <= 1) return <div className="pager muted">共 {total} 条</div>;
  return (
    <div className="pager">
      <span className="muted">共 {total} 条 · 第 {page} / {pages} 页</span>
      <div className="pager-btns">
        <button className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))} disabled={page <= 1} onClick={() => onChange(page - 1)}>
          上一页
        </button>
        <button className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))} disabled={page >= pages} onClick={() => onChange(page + 1)}>
          下一页
        </button>
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

/** 解析审计里的 before/after JSON；坏数据按空对象处理（历史不应让页面崩）。 */
function parseKV(raw: string): Record<string, string> {
  try {
    const v = JSON.parse(raw || '{}') as unknown;
    return v && typeof v === 'object' ? (v as Record<string, string>) : {};
  } catch {
    return {};
  }
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

/**
 * 登录错误展示：错误信封的 message + code/name + trace_id 都必须可见、可复制（AGENTS §8.9）。
 * 生产 UI 不展示任何开发口令或关闭 MFA 的提示（ADM-UI-03）。
 */
export function LoginError({ error }: { error: unknown }) {
  const meta = errorMeta(error, false);
  return (
    <Alert variant="destructive">
      <AlertTitle>{meta.message}</AlertTitle>
      <AlertDescription className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
        <span className="font-mono">
          {meta.code} {meta.name}
        </span>
        {meta.traceId ? (
          <button
            type="button"
            className="underline underline-offset-2"
            onClick={() => void navigator.clipboard?.writeText(meta.traceId)}
          >
            复制 trace_id
          </button>
        ) : null}
      </AlertDescription>
    </Alert>
  );
}

export function Login({ onDone }: { onDone: () => void }) {
  const [username, setUsername] = useState('');
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
      setIdentity({ username: res.username, role: res.role, permissions: res.permissions });
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
        <UiCard className="w-[380px] max-w-full">
          <UiCardHeader>
            <UiCardTitle className="text-2xl">登录控制台</UiCardTitle>
            <UiCardDescription>仅限授权人员访问，所有操作均写入审计</UiCardDescription>
          </UiCardHeader>
          <UiCardContent>
            <form className="grid gap-4" onSubmit={submit} noValidate>
              {error !== null && <LoginError error={error} />}
              <div className="grid gap-2">
                <Label htmlFor="login-username">账号</Label>
                <Input
                  id="login-username"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  autoComplete="username"
                  disabled={busy}
                  required
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="login-password">密码</Label>
                <Input
                  id="login-password"
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  autoComplete="current-password"
                  disabled={busy}
                  required
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="login-totp">二次验证码</Label>
                <Input
                  id="login-totp"
                  inputMode="numeric"
                  maxLength={6}
                  value={totp}
                  onChange={(e) => setTotp(e.target.value)}
                  placeholder="6 位数字"
                  autoComplete="one-time-code"
                  disabled={busy}
                />
              </div>
              <Button type="submit" className="h-11 w-full" disabled={busy}>
                {busy ? '登录中…' : '登录'}
              </Button>
              <p className="text-xs text-muted-foreground">仅限已授权的运营与客服账号 · 二次验证强制开启</p>
            </form>
          </UiCardContent>
        </UiCard>
      </main>
    </div>
  );
}

/* ---------- 仪表盘 ---------- */

function Dashboard({ onOpen, refreshKey }: { onOpen: (p: PageId) => void; refreshKey: number }) {
  const [overview, setOverview] = useState<Overview | null>(null);
  const [trend, setTrend] = useState<TrendPoint[]>([]);
  const [devices, setDevices] = useState<DeviceRow[]>([]);
  const [sessions, setSessions] = useState<SessionRow[]>([]);
  const [audit, setAudit] = useState<AuditRow[]>([]);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [auto, setAuto] = useState(true);
  const [range, setRange] = useState<'24h' | '7d' | '30d'>('24h');
  const [lastUpdatedAt, setLastUpdatedAt] = useState<number | null>(null);
  const [now, setNow] = useState(() => Date.now());

  const load = useCallback(async () => {
    try {
      const [ov, tr, dv, ss, au] = await Promise.all([
        api.overview(range),
        api.trends(range, range === '24h' ? 'hour' : 'day'),
        api.devices(),
        api.sessions(),
        api.audit(),
      ]);
      setOverview(ov);
      setTrend(tr.points);
      setDevices(dv.items);
      setSessions(ss.items);
      setAudit(au.items);
      setLastUpdatedAt(Date.now());
      setError(null);
    } catch (err) {
      // 失败时保留上一次数据，只把状态标成陈旧；清空页面等于谎报"没有数据"。
      setError(err);
    } finally {
      setLoading(false);
    }
  }, [range]);

  useEffect(() => {
    void load();
  }, [load, refreshKey]);

  // PRD §6.3.1：5s 刷新。暂停或卸载必须清掉定时器，避免叠加请求。
  useEffect(() => {
    if (!auto) return;
    const t = window.setInterval(() => void load(), POLL_INTERVAL_MS);
    return () => window.clearInterval(t);
  }, [auto, load]);

  // 每秒重算"是否陈旧"：没有这个 tick，暂停后界面不会自己变陈旧。
  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now()), 1_000);
    return () => window.clearInterval(t);
  }, []);

  const platforms = useMemo(() => {
    const map = new Map<string, number>();
    for (const d of devices) map.set(d.platform || 'unknown', (map.get(d.platform || 'unknown') ?? 0) + 1);
    return [...map.entries()].sort((a, b) => b[1] - a[1]);
  }, [devices]);

  const groups = buildMetricGroups(overview);
  const rate = onlineRate(overview);
  const stale = isStale(lastUpdatedAt, now);

  if (loading) return <LoadingState />;

  return (
    <div className="stack">
      <ErrorState error={error} onRetry={() => void load()} />
      <div className="flex flex-wrap items-center justify-between gap-3 text-sm">
        <div className="flex flex-wrap items-center gap-3">
          <span className="text-muted-foreground">更新于 {formatUpdatedAt(lastUpdatedAt)}</span>
          {stale ? (
            <span
              data-testid="stale-badge"
              className="rounded-full bg-[var(--warn-bg)] px-2 py-0.5 text-xs text-[var(--warn)]"
            >
              数据已陈旧
            </span>
          ) : null}
          <span className="text-xs text-muted-foreground">每 5 秒刷新</span>
        </div>
        <div className="flex items-center gap-2">
          <Select
            value={range}
            onValueChange={(v) => setRange(v as '24h' | '7d' | '30d')}
          >
            <SelectTrigger aria-label="质量统计窗口" className="w-[170px]"><SelectValue /></SelectTrigger>
            <SelectContent>
            <SelectItem value="24h" data-value="24h">24 小时</SelectItem>
            <SelectItem value="7d" data-value="7d">7 天</SelectItem>
            <SelectItem value="30d" data-value="30d">30 天</SelectItem>
          </SelectContent></Select>
          <label className="flex items-center gap-2 text-xs text-muted-foreground">
              <Switch checked={auto} onCheckedChange={setAuto} aria-label="自动刷新" /> 自动刷新
            </label>
          <button className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))} onClick={() => void load()}>
            刷新
          </button>
        </div>
      </div>

      {groups.map((group) => (
        <section key={group.id} className="stack" aria-label={group.title} data-group={group.id}>
          <div className="flex flex-wrap items-baseline justify-between gap-2">
            <h3 className="text-sm font-semibold">{group.title}</h3>
            <span className="text-xs text-muted-foreground">{group.subtitle}</span>
          </div>
          <div className="metrics">
            {group.items.map((item) => {
              const body = (
                <>
                  <span>{item.label}</span>
                  <strong className={item.value === null ? 'text-base text-muted-foreground' : undefined}>
                    {item.value ?? UNAVAILABLE_HINT}
                  </strong>
                  {item.hint && item.hint !== UNAVAILABLE_HINT ? <em>{item.hint}</em> : null}
                </>
              );
              return item.page && item.value !== null ? (
                <button key={item.key} className="metric" data-metric={item.key} onClick={() => onOpen(item.page!)}>
                  {body}
                </button>
              ) : (
                <div key={item.key} className={'metric' + (item.value === null ? ' muted' : '')} data-metric={item.key}>
                  {body}
                </div>
              );
            })}
          </div>
        </section>
      ))}

      <Card
        title="趋势"
        subtitle={
          '窗口 ' + range + ' · ' + (range === '24h' ? '按小时' : '按天') + ' · ' + trend.length + ' 个时间桶'
        }
      >
        {trend.length === 0 ? (
          <EmptyState text="窗口内没有可统计的事件。" hint="会话开始与建连尝试都来自真实事件表。" />
        ) : (
          <div className="grid gap-3">
            {(
              [
                { key: 'sessions_started', label: '会话开始' },
                { key: 'connect_attempts', label: '建连尝试' },
                { key: 'connect_active', label: '建连成功' },
              ] as const
            ).map((s) => (
              <div key={s.key} className="grid gap-1">
                <div className="flex items-center justify-between text-xs text-muted-foreground">
                  <span>{s.label}</span>
                  <span>峰值 {Math.max(...trend.map((p) => p[s.key]))}</span>
                </div>
                <svg role="img" aria-label={'趋势 ' + s.label} viewBox="0 0 320 64" width="100%" height="64">
                  <polyline
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                    points={trendPath(trend, s.key)}
                  />
                </svg>
              </div>
            ))}
          </div>
        )}
      </Card>

      <div className="grid-2">
        <Card
          title="在线率"
          subtitle={
            overview ? '设备 ' + overview.devices_total + ' 台 · 在线 ' + overview.devices_online + ' 台' : '等待数据'
          }
        >
          {rate === null ? (
            <EmptyState text="设备总数为 0，暂无在线率" />
          ) : (
            <>
              <div className="bar">
                <div className="bar-fill" style={{ width: rate + '%' }} />
              </div>
              <div className="bar-legend">
                <span>在线 {rate}%</span>
                <span className="muted">离线 {100 - rate}%</span>
              </div>
            </>
          )}
        </Card>
        <Card title="平台分布" subtitle={'本页 ' + devices.length + ' 台样本 · 全量分布接口未提供'}>
          {platforms.length === 0 ? (
            <EmptyState text="暂无设备" />
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
          subtitle={'更新于 ' + formatUpdatedAt(lastUpdatedAt)}
          actions={
            <label className="flex items-center gap-2 text-xs text-muted-foreground">
              <Switch checked={auto} onCheckedChange={setAuto} aria-label="自动刷新" /> 自动刷新
            </label>
          }
        >
          {sessions.length === 0 ? (
            <EmptyState text="暂无会话" />
          ) : (
            <Table aria-label="最近会话">
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>会话</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>时长</TableHead>
                  <TableHead>开始时间</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {sessions.slice(0, 8).map((s) => (
                  <TableRow key={s.id}>
                    <TableCell className="font-mono text-xs">{s.id}</TableCell>
                    <TableCell>
                      <Pill tone={s.state === 'active' ? 'ok' : s.state === 'requested' ? 'warn' : 'muted'}>{s.state}</Pill>
                    </TableCell>
                    <TableCell className="text-[13px]">{duration(s)}</TableCell>
                    <TableCell className="text-[13px] text-muted-foreground">{fmtTime(s.started_at)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </Card>
        <Card title="最近审计" subtitle="登录、封禁、中断等敏感操作">
          {audit.length === 0 ? (
            <EmptyState text="暂无记录" />
          ) : (
            <Table aria-label="最近审计">
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>动作</TableHead>
                  <TableHead>操作人</TableHead>
                  <TableHead>结果</TableHead>
                  <TableHead>时间</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {audit.slice(0, 8).map((a) => (
                  <TableRow key={a.id}>
                    <TableCell className="font-mono text-xs">{a.action}</TableCell>
                    <TableCell className="text-[13px]">{a.actor}</TableCell>
                    <TableCell>
                      <Pill tone={a.result === 'ok' ? 'ok' : 'danger'}>{a.result}</Pill>
                    </TableCell>
                    <TableCell className="text-[13px] text-muted-foreground">{relTime(a.ts)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </Card>
      </div>
    </div>
  );
}

/* ---------- 用户 ---------- */

function Users({ notice }: { notice: (t: string) => void }) {
  const canBan = useCan('user.ban');
  const navigate = useNavigate();
  const [rows, setRows] = useState<UserRow[]>([]);
  const [total, setTotal] = useState(0);
  const [query, setQuery] = useState('');
  const [status, setStatus] = useState('all');
  const [page, setPage] = useState(1);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [target, setTarget] = useState<UserRow | null>(null);
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [action, setAction] = useState<{ kind: 'unban' | 'kick'; user: UserRow } | null>(null);
  const [actionReason, setActionReason] = useState('');
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');
  const [exporting, setExporting] = useState(false);
  const [exportInfo, setExportInfo] = useState('');

  async function exportCsv() {
    setExporting(true);
    setError(null);
    try {
      const res = await api.exportUsers({
        q: query.trim() || undefined,
        status: status === 'all' ? undefined : status,
        from: from ? new Date(from).toISOString() : undefined,
        to: to ? new Date(to).toISOString() : undefined,
      });
      const blob = new Blob(['\uFEFF' + res.csv], { type: 'text/csv;charset=utf-8' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = 'users-export.csv';
      a.click();
      URL.revokeObjectURL(url);
      setExportInfo('已导出 ' + res.rows + ' 行' + (res.truncated ? '（达到上限，已截断）' : ''));
    } catch (err) {
      setError(err);
    } finally {
      setExporting(false);
    }
  }

  async function runAction() {
    if (!action || actionReason.trim().length < 2) return;
    setBusy(true);
    setError(null);
    try {
      if (action.kind === 'unban') {
        await api.unbanUser(action.user.id, actionReason.trim());
        notice('已解封 ' + action.user.id);
      } else {
        await api.kickUser(action.user.id, actionReason.trim());
        notice('已踢下线 ' + action.user.id);
      }
      setAction(null);
      setActionReason('');
      await load();
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }

  const load = useCallback(async () => {
    try {
      setLoading(true);
      const offset = (page - 1) * PAGE_SIZE;
      const res = await api.users({
        limit: PAGE_SIZE,
        offset,
        q: query.trim() || undefined,
        status: status === 'all' ? undefined : status,
        from: from ? new Date(from).toISOString() : undefined,
        to: to ? new Date(to).toISOString() : undefined,
      });
      setRows(res.items);
      setTotal(res.total);
      setError(null);
    } catch (err) {
      setError(err);
    } finally {
      setLoading(false);
    }
  }, [page, query, status, from, to]);

  useEffect(() => {
    void load();
  }, [load]);


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
      <ErrorState error={error} />
      {exportInfo ? <div className="banner ok"><span>{exportInfo}</span></div> : null}
      <Card
        title={'用户 (' + total + ')'}
        subtitle={'今日新增 ' + rows.filter((u) => Date.now() - new Date(u.created_at).getTime() < 864e5).length + ' · 受限 ' + rows.filter((u) => u.status !== 'active').length}
        actions={
          <>
            <Input
              placeholder="搜索账号或手机号掩码"
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
                setPage(1);
              }}
            />
            <Select
              value={status}
              onValueChange={(v) => {
                setStatus(v);
                setPage(1);
              }}
            >
              <SelectTrigger aria-label="选择" className="w-[170px]"><SelectValue /></SelectTrigger>
              <SelectContent>
              <SelectItem value="all" data-value="all">全部状态</SelectItem>
              <SelectItem value="active" data-value="active">active</SelectItem>
              <SelectItem value="banned" data-value="banned">banned</SelectItem>
            </SelectContent></Select>
            <Input
              type="datetime-local"
              aria-label="注册开始时间"
              value={from}
              onChange={(e) => {
                setFrom(e.target.value);
                setPage(1);
              }}
            />
            <Input
              type="datetime-local"
              aria-label="注册结束时间"
              value={to}
              onChange={(e) => {
                setTo(e.target.value);
                setPage(1);
              }}
            />
            <button
              className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))}
              disabled={exporting}
              title="按当前筛选导出 CSV（服务端脱敏 + 写审计）"
              onClick={() => void exportCsv()}
            >
              {exporting ? '导出中…' : '导出 CSV'}
            </button>
          </>
        }
      >
        {loading ? (
          <LoadingState />
        ) : (
          <DataTable
            ariaLabel="用户列表"
            rows={rows}
            rowKey={(u) => u.id}
            rowTitle="打开用户详情"
            onRowClick={(u) => navigate('/users/' + u.id)}
            empty={<EmptyState text="没有匹配的用户" />}
            columns={[
              { key: 'id', header: '账号 ID', className: 'mono', render: (u) => u.id },
              {
                key: 'nickname',
                header: '昵称',
                render: (u) => u.nickname || <span className="muted">—</span>,
              },
              { key: 'phone', header: '手机号', render: (u) => u.phone_mask },
              { key: 'devices', header: '设备数', render: (u) => String(u.device_count) },
              {
                key: 'status',
                header: '状态',
                render: (u) => <Pill tone={u.status === 'active' ? 'ok' : 'danger'}>{u.status}</Pill>,
              },
              { key: 'created', header: '注册时间', className: 'muted', render: (u) => fmtTime(u.created_at) },
              {
                key: 'last_login',
                header: '最近登录',
                className: 'muted',
                render: (u) => relTime(u.last_login_at ?? ''),
              },
              {
                key: 'actions',
                header: '操作',
                className: 'right',
                render: (u) => (
                  <span className="flex justify-end gap-2">
                    <button className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))} onClick={() => navigate('/users/' + u.id)}>
                      详情
                    </button>
                    {canBan ? (
                      <button
                        className={cn(buttonVariants({ variant: 'destructive', size: 'sm' }))}
                        disabled={u.status !== 'active'}
                        onClick={(e) => {
                          e.stopPropagation();
                          setTarget(u);
                        }}
                      >
                        封禁
                      </button>
                    ) : null}
                    {canBan && u.status !== 'active' ? (
                      <button
                        className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))}
                        onClick={(e) => {
                          e.stopPropagation();
                          setAction({ kind: 'unban', user: u });
                          setActionReason('');
                        }}
                      >
                        解封
                      </button>
                    ) : null}
                    {canBan ? (
                      <button
                        className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))}
                        title="关闭该账号所有设备参与的会话并置离线"
                        onClick={(e) => {
                          e.stopPropagation();
                          setAction({ kind: 'kick', user: u });
                          setActionReason('');
                        }}
                      >
                        踢下线
                      </button>
                    ) : null}
                  </span>
                ),
              },
            ]}
          />
        )}
        <Pagination page={page} total={total} onChange={setPage} />
      </Card>

      <ConfirmDialog
        open={action !== null}
        title={
          action
            ? (action.kind === 'unban' ? '解封 ' : '踢下线 ') + action.user.id
            : '账号操作'
        }
        description={
          action?.kind === 'unban'
            ? '解封后该账号可以重新建立会话；操作写入审计。'
            : '会关闭该账号所有设备参与的会话并置离线；操作写入审计。'
        }
        confirmLabel={action?.kind === 'unban' ? '确认解封' : '确认踢下线'}
        danger={action?.kind === 'kick'}
        busy={busy}
        confirmDisabled={actionReason.trim().length < 2}
        onConfirm={() => void runAction()}
        onClose={() => {
          setAction(null);
          setActionReason('');
        }}
      >
        <div className="grid gap-2">
          <Label htmlFor="user-action-reason">原因（至少 2 个字）</Label>
          <Textarea
            id="user-action-reason"
            rows={2}
            value={actionReason}
            onChange={(e) => setActionReason(e.target.value)}
          />
        </div>
      </ConfirmDialog>

      <ConfirmDialog
        open={target !== null}
        title={target ? '封禁 ' + target.id : '封禁'}
        description="封禁后该账号的所有设备将无法建立会话，操作会写入审计。"
        confirmLabel="确认封禁"
        danger
        busy={busy}
        confirmDisabled={reason.trim().length < 4}
        onConfirm={() => void ban()}
        onClose={() => {
          setTarget(null);
          setReason('');
        }}
      >
        <div className="grid gap-2">
          <Label htmlFor="ban-reason">原因（至少 4 个字）</Label>
          <Textarea id="ban-reason" rows={3} value={reason} onChange={(e) => setReason(e.target.value)} />
        </div>
      </ConfirmDialog>
    </div>
  );
}

/* ---------- 设备 ---------- */

function Devices({ notice }: { notice: (t: string) => void }) {
  const navigate = useNavigate();
  const canForce = useCan('device.force_offline');
  const canUnbind = useCan('device.unbind');
  const canFlagRisk = useCan('device.flag_risk');
  const [rows, setRows] = useState<DeviceRow[]>([]);
  const [total, setTotal] = useState(0);
  const [query, setQuery] = useState('');
  const [platform, setPlatform] = useState('all');
  const [onlineFilter, setOnlineFilter] = useState('all');
  const [page, setPage] = useState(1);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [action, setAction] = useState<{ kind: 'force' | 'unbind' | 'flag' | 'clear'; device: DeviceRow } | null>(
    null,
  );
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      setLoading(true);
      const offset = (page - 1) * PAGE_SIZE;
      const res = await api.devices({
        limit: PAGE_SIZE,
        offset,
        q: query.trim() || undefined,
        platform: platform === 'all' ? undefined : platform,
        online: onlineFilter === 'all' ? undefined : onlineFilter === 'online',
      });
      setRows(res.items);
      setTotal(res.total);
      setError(null);
    } catch (err) {
      setError(err);
    } finally {
      setLoading(false);
    }
  }, [page, query, platform, onlineFilter]);

  useEffect(() => {
    void load();
  }, [load]);

  const platforms = useMemo(() => [...new Set(rows.map((d) => d.platform))], [rows]);

  async function runAction() {
    if (!action || reason.trim().length < 2) return;
    setBusy(true);
    try {
      if (action.kind === 'force') {
        await api.forceOffline(action.device.id, reason.trim());
        notice('已强制下线 ' + action.device.device_code);
      } else if (action.kind === 'flag') {
        await api.flagDeviceRisk(action.device.id, reason.trim());
        notice('已标记风险 ' + action.device.device_code);
      } else if (action.kind === 'clear') {
        await api.clearDeviceRisk(action.device.id, reason.trim());
        notice('已解除风险 ' + action.device.device_code);
      } else {
        await api.unbindDevice(action.device.id, reason.trim());
        notice('已解绑 ' + action.device.device_code);
      }
      setAction(null);
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
      <ErrorState error={error} />
      <Card
        title={'设备 (' + total + ')'}
        subtitle={'本页在线 ' + rows.filter((d) => d.online).length + ' 台 · 平台 ' + platforms.length + ' 种'}
        actions={
          <>
            <Input
              placeholder="搜索设备码 / 账号 / 平台"
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
                setPage(1);
              }}
            />
            <Select
              value={platform}
              onValueChange={(v) => {
                setPlatform(v);
                setPage(1);
              }}
            >
              <SelectTrigger aria-label="选择" className="w-[170px]"><SelectValue /></SelectTrigger>
              <SelectContent>
              <SelectItem value="all" data-value="all">全部平台</SelectItem>
              {platforms.map((p) => (
                <SelectItem key={p} value={p} data-value={p}>
                  {p}
                </SelectItem>
              ))}
            </SelectContent></Select>
            <Select
              value={onlineFilter}
              onValueChange={(v) => {
                setOnlineFilter(v);
                setPage(1);
              }}
            >
              <SelectTrigger aria-label="在线状态筛选" className="w-[170px]"><SelectValue /></SelectTrigger>
              <SelectContent>
              <SelectItem value="all" data-value="all">全部在线状态</SelectItem>
              <SelectItem value="online" data-value="online">在线</SelectItem>
              <SelectItem value="offline" data-value="offline">离线</SelectItem>
            </SelectContent></Select>
            <button className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))} onClick={() => void load()}>
              刷新
            </button>
          </>
        }
      >
        {loading ? (
          <LoadingState />
        ) : (
          <DataTable
            ariaLabel="设备列表"
            rows={rows}
            rowKey={(d) => d.id || d.device_code}
            rowTitle="打开设备详情"
            onRowClick={(d) => navigate('/devices/' + d.id)}
            empty={<EmptyState text="没有匹配的设备" />}
            columns={[
              { key: 'code', header: '设备码', className: 'mono big', render: (d) => fmtCode(d.device_code) },
              {
                key: 'name',
                header: '设备名',
                render: (d) => d.device_name || <span className="muted">—</span>,
              },
              { key: 'account', header: '归属账号', className: 'mono', render: (d) => d.account_id },
              { key: 'platform', header: '平台', render: (d) => d.platform },
              { key: 'os', header: '系统版本', className: 'muted', render: (d) => d.os_version || '—' },
              { key: 'client', header: '客户端版本', className: 'muted', render: (d) => d.client_version || '—' },
              { key: 'nat', header: 'NAT', className: 'muted', render: (d) => d.nat_type || '—' },
              {
                key: 'risk',
                header: '风险',
                render: (d) =>
                  d.risk_flag ? <Pill tone="danger">风险</Pill> : <span className="muted">正常</span>,
              },
              {
                key: 'online',
                header: '在线',
                render: (d) => <Pill tone={d.online ? 'ok' : 'muted'}>{d.online ? '在线' : '离线'}</Pill>,
              },
              {
                key: 'seen',
                header: '最后在线',
                className: 'muted',
                render: (d) => relTime(d.last_seen || d.last_seen_at || ''),
              },
              {
                key: 'actions',
                header: '操作',
                className: 'right',
                render: (d) => (
                  <span className="flex justify-end gap-2">
                    <button
                      className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))}
                      onClick={(e) => {
                        e.stopPropagation();
                        void navigator.clipboard?.writeText(d.device_code.replace(/\s/g, ''));
                        notice('设备码已复制');
                      }}
                    >
                      复制设备码
                    </button>
                    {canForce ? (
                      <button
                        className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))}
                        disabled={!d.online}
                        title={d.online ? '强制下线（需填原因）' : '设备已离线'}
                        onClick={(e) => {
                          e.stopPropagation();
                          setAction({ kind: 'force', device: d });
                          setReason('');
                        }}
                      >
                        强制下线
                      </button>
                    ) : null}
                    {canFlagRisk ? (
                      <button
                        className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))}
                        title={d.risk_flag ? '解除风险标记（需填原因）' : '标记风险设备（需填原因）'}
                        onClick={(e) => {
                          e.stopPropagation();
                          setAction({ kind: d.risk_flag ? 'clear' : 'flag', device: d });
                          setReason('');
                        }}
                      >
                        {d.risk_flag ? '解除风险' : '标记风险'}
                      </button>
                    ) : null}
                    {canUnbind ? (
                      <button
                        className={cn(buttonVariants({ variant: 'destructive', size: 'sm' }))}
                        title="解绑设备（需填原因，仅超级管理员）"
                        onClick={(e) => {
                          e.stopPropagation();
                          setAction({ kind: 'unbind', device: d });
                          setReason('');
                        }}
                      >
                        解绑
                      </button>
                    ) : null}
                  </span>
                ),
              },
            ]}
          />
        )}
        <Pagination page={page} total={total} onChange={setPage} />
      </Card>
      <p className="text-xs text-muted-foreground">
        设备名/系统版本/客户端版本/NAT 由客户端绑定上报（ADM-API-04a），「—」表示尚未上报；在线状态筛选由服务端按 presence 过滤。
      </p>

      <ConfirmDialog
        open={action !== null}
        title={
          action
            ? { force: '强制下线 ', unbind: '解绑 ', flag: '标记风险 ', clear: '解除风险 ' }[action.kind] +
              action.device.device_code
            : '设备操作'
        }
        description={
          action?.kind === 'force'
            ? '会关闭该设备参与的未结束会话并标记离线；操作写入审计。'
            : action?.kind === 'unbind'
              ? '解绑会删除设备记录，设备需要重新绑定；操作写入审计。'
              : action?.kind === 'flag'
                ? '标记后列表会显示风险徽标，操作写入审计。'
                : '解除后风险徽标消失，操作写入审计。'
        }
        confirmLabel={
          action ? { force: '确认下线', unbind: '确认解绑', flag: '确认标记', clear: '确认解除' }[action.kind] : '确认'
        }
        danger
        busy={busy}
        confirmDisabled={reason.trim().length < 2}
        onConfirm={() => void runAction()}
        onClose={() => {
          setAction(null);
          setReason('');
        }}
      >
        <div className="grid gap-2">
          <Label htmlFor="device-reason">原因（至少 2 个字）</Label>
          <Textarea id="device-reason" rows={2} value={reason} onChange={(e) => setReason(e.target.value)} />
        </div>
      </ConfirmDialog>
    </div>
  );
}

/* ---------- 会话 ---------- */

function Sessions({ notice }: { notice: (t: string) => void }) {
  const canKill = useCan('session.kill');
  const navigate = useNavigate();
  const [rows, setRows] = useState<SessionRow[]>([]);
  const [total, setTotal] = useState(0);
  const [state, setState] = useState('all');
  const [page, setPage] = useState(1);
  const [auto, setAuto] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [target, setTarget] = useState<SessionRow | null>(null);
  const [category, setCategory] = useState<string>('');
  const [detail, setDetail] = useState('');
  const [busy, setBusy] = useState(false);
  // 双人确认中断（ADM-UI-07b2）：发起请求与待确认列表。
  const [pending, setPending] = useState<KillRequestRow[]>([]);
  const [dualTarget, setDualTarget] = useState<SessionRow | null>(null);
  const [dualReason, setDualReason] = useState('');
  const [confirming, setConfirming] = useState<KillRequestRow | null>(null);
  // 会话筛选（ADM-API-03f1）：时间范围 / 账号 / 设备，全部下推服务端。
  const [accountId, setAccountId] = useState('');
  const [deviceId, setDeviceId] = useState('');
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');
  // ADM-API-03f2：链路类型与异常结束筛选。
  const [linkFilter, setLinkFilter] = useState('all');
  const [abnormalFilter, setAbnormalFilter] = useState('all');

  const load = useCallback(async () => {
    try {
      setLoading(true);
      const offset = (page - 1) * PAGE_SIZE;
      const [res, reqs] = await Promise.all([
        api.sessions(state === 'all' ? undefined : state, {
          limit: PAGE_SIZE,
          offset,
          account_id: accountId.trim() || undefined,
          device_id: deviceId.trim() || undefined,
          from: from ? new Date(from).toISOString() : undefined,
          to: to ? new Date(to).toISOString() : undefined,
          link: linkFilter === 'all' ? undefined : linkFilter,
          abnormal: abnormalFilter === 'all' ? undefined : abnormalFilter === 'abnormal',
        }),
        api.killRequests('pending').catch(() => ({ items: [], total: 0 })),
      ]);
      setRows(res.items);
      setTotal(res.total);
      setPending(reqs.items);
      setError(null);
    } catch (err) {
      setError(err);
    } finally {
      setLoading(false);
    }
  }, [state, page, accountId, deviceId, from, to, linkFilter, abnormalFilter]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (!auto) return;
    const t = window.setInterval(() => void load(), 15_000);
    return () => window.clearInterval(t);
  }, [auto, load]);

  const reason = composeKillReason(category, detail);
  const reasonValid = isKillReasonValid(category, detail);

  async function kill() {
    if (!target || !reasonValid) return;
    setBusy(true);
    try {
      const res = await api.killSession(target.id, reason);
      // ER-812 语义：关闭 / 传播 / 审计是三个独立结果，不能合并成一个"成功"。
      const outcome = describeKillResult(res, target.id);
      if (outcome.tone === 'ok') {
        notice(outcome.message);
      } else {
        notice(outcome.message);
        setError(new Error(outcome.detail ?? outcome.message));
      }
      setTarget(null);
      setCategory('');
      setDetail('');
      await load();
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') {
        setError(new Error('中断请求超时：结果未知，请刷新列表确认，并复制 trace_id 排查'));
      } else {
        setError(err);
      }
    } finally {
      setBusy(false);
    }
  }

  async function submitDualRequest() {
    if (!dualTarget || dualReason.trim().length < 2) return;
    setBusy(true);
    setError(null);
    try {
      await api.killRequest(dualTarget.id, dualReason.trim());
      notice('已发起双人中断请求，等待另一位管理员确认');
      setDualTarget(null);
      setDualReason('');
      await load();
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }

  async function submitConfirm() {
    if (!confirming) return;
    setBusy(true);
    setError(null);
    try {
      const res = await api.killConfirm(confirming.id);
      notice(res.closed ? '已确认并中断会话' : '确认成功，但会话状态未关闭');
      setConfirming(null);
      await load();
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="stack">
      <ErrorState error={error} />
      <Card
        title={'会话 (' + total + ')'}
        subtitle="每 15 秒自动刷新 · 中断以服务端关闭/传播/审计结果为准"
        actions={
          <>
            <Input
              aria-label="账号筛选"
              placeholder="账号 ID"
              value={accountId}
              onChange={(e) => {
                setAccountId(e.target.value);
                setPage(1);
              }}
            />
            <Input
              aria-label="设备筛选"
              placeholder="设备 ID"
              value={deviceId}
              onChange={(e) => {
                setDeviceId(e.target.value);
                setPage(1);
              }}
            />
            <Input
              type="datetime-local"
              aria-label="开始时间范围"
              value={from}
              onChange={(e) => {
                setFrom(e.target.value);
                setPage(1);
              }}
            />
            <Input
              type="datetime-local"
              aria-label="结束时间范围"
              value={to}
              onChange={(e) => {
                setTo(e.target.value);
                setPage(1);
              }}
            />
            <Select
              value={linkFilter}
              onValueChange={(v) => {
                setLinkFilter(v);
                setPage(1);
              }}
            >
              <SelectTrigger aria-label="链路筛选" className="w-[170px]"><SelectValue /></SelectTrigger>
              <SelectContent>
              <SelectItem value="all" data-value="all">全部链路</SelectItem>
              <SelectItem value="p2p" data-value="p2p">P2P</SelectItem>
              <SelectItem value="relay" data-value="relay">中继</SelectItem>
              <SelectItem value="mixed" data-value="mixed">混合</SelectItem>
            </SelectContent></Select>
            <Select
              value={abnormalFilter}
              onValueChange={(v) => {
                setAbnormalFilter(v);
                setPage(1);
              }}
            >
              <SelectTrigger aria-label="结束类型筛选" className="w-[170px]"><SelectValue /></SelectTrigger>
              <SelectContent>
              <SelectItem value="all" data-value="all">全部结束类型</SelectItem>
              <SelectItem value="abnormal" data-value="abnormal">异常结束</SelectItem>
              <SelectItem value="normal" data-value="normal">正常结束</SelectItem>
            </SelectContent></Select>
            <Select
              value={state}
              onValueChange={(v) => {
                setState(v);
                setPage(1);
              }}
            >
              <SelectTrigger aria-label="状态筛选" className="w-[170px]"><SelectValue /></SelectTrigger>
              <SelectContent>
              <SelectItem value="all" data-value="all">全部状态</SelectItem>
              {SESSION_STATES.map((s) => (
                <SelectItem key={s} value={s} data-value={s}>
                  {stateLabel(s)}（{s}）
                </SelectItem>
              ))}
            </SelectContent></Select>
            <label className="flex items-center gap-2 text-xs text-muted-foreground">
              <Switch checked={auto} onCheckedChange={setAuto} aria-label="自动刷新" /> 自动刷新
            </label>
            <button className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))} onClick={() => void load()}>
              刷新
            </button>
          </>
        }
      >
        {loading ? (
          <LoadingState />
        ) : (
          <DataTable
            ariaLabel="会话列表"
            rows={rows}
            rowKey={(s) => s.id}
            rowTitle="打开会话详情"
            onRowClick={(s) => navigate('/sessions/' + s.id)}
            empty={<EmptyState text="暂无会话" />}
            columns={[
              { key: 'id', header: '会话 ID', className: 'mono', render: (s) => s.id },
              { key: 'controller', header: '主控', className: 'mono', render: (s) => s.controller_id },
              { key: 'target', header: '被控', className: 'mono', render: (s) => s.target_id },
              {
                key: 'state',
                header: '状态',
                render: (s) => <Pill tone={stateTone(s.state)}>{stateLabel(s.state)}</Pill>,
              },
              { key: 'duration', header: '时长', render: (s) => duration(s) },
              {
                key: 'latency',
                header: '平均延迟',
                className: 'muted',
                render: (s) => (s.latency_available ? s.avg_latency_ms + ' ms' : '—'),
              },
              {
                key: 'reason',
                header: '结束原因',
                className: 'muted',
                render: (s) => (s.reason ? s.reason : <span className="muted">—</span>),
              },
              {
                key: 'link',
                header: '链路',
                render: (s) =>
                  s.link ? (
                    <Pill tone={s.link === 'p2p' ? 'ok' : s.link === 'relay' ? 'warn' : 'muted'}>
                      {s.link === 'p2p' ? 'P2P' : s.link === 'relay' ? '中继' : '混合'}
                    </Pill>
                  ) : (
                    <span className="muted">—</span>
                  ),
              },
              { key: 'started', header: '开始时间', className: 'muted', render: (s) => fmtTime(s.started_at) },
              {
                key: 'actions',
                header: '操作',
                className: 'right',
                render: (s) => (
                  <span className="flex justify-end gap-2">
                    <button className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))} onClick={() => navigate('/sessions/' + s.id)}>
                      详情
                    </button>
                    {canKill ? (
                      <button
                        className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))}
                        disabled={isTerminal(s.state)}
                        title={isTerminal(s.state) ? '会话已结束' : '发起双人中断（需另一位管理员确认）'}
                        onClick={(e) => {
                          e.stopPropagation();
                          setDualTarget(s);
                          setDualReason('');
                        }}
                      >
                        双人中断
                      </button>
                    ) : null}
                    {canKill ? (
                      <button
                        className={cn(buttonVariants({ variant: 'destructive', size: 'sm' }))}
                        disabled={isTerminal(s.state)}
                        title={isTerminal(s.state) ? '会话已结束，无需中断' : '强制中断会话'}
                        onClick={(e) => {
                          e.stopPropagation();
                          setTarget(s);
                          setCategory('');
                          setDetail('');
                        }}
                      >
                        中断
                      </button>
                    ) : null}
                  </span>
                ),
              },
            ]}
          />
        )}
        <Pagination page={page} total={total} onChange={setPage} />
      </Card>

      {canKill && pending.length > 0 ? (
        <Card
          title={'待确认中断 (' + pending.length + ')'}
          subtitle="双人确认：发起人不能自己确认，5 分钟内有效（ADM-API-07b1）"
        >
          <DataTable
            ariaLabel="待确认中断"
            rows={pending}
            rowKey={(r) => r.id}
            columns={[
              { key: 'session', header: '会话', className: 'mono', render: (r) => r.session_id },
              { key: 'by', header: '发起人', render: (r) => r.requested_by },
              { key: 'reason', header: '原因', className: 'muted', render: (r) => r.reason },
              { key: 'expires', header: '过期时间', className: 'muted', render: (r) => fmtTime(r.expires_at) },
              {
                key: 'actions',
                header: '操作',
                render: (r) => (
                  <button
                    className={cn(buttonVariants({ variant: 'destructive', size: 'sm' }))}
                    onClick={() => {
                      setConfirming(r);
                    }}
                  >
                    确认中断
                  </button>
                ),
              },
            ]}
          />
        </Card>
      ) : null}

      <ConfirmDialog
        open={confirming !== null}
        title={confirming ? '确认中断 ' + confirming.session_id : '确认中断'}
        description="确认后会话会立即中断，并写入审计；发起人不能自己确认。"
        confirmLabel="确认中断"
        danger
        busy={busy}
        onConfirm={() => void submitConfirm()}
        onClose={() => setConfirming(null)}
      />

      <ConfirmDialog
        open={dualTarget !== null}
        title={dualTarget ? '发起双人中断 ' + dualTarget.id : '双人中断'}
        description="提交后需要另一位管理员在「待确认中断」里确认，5 分钟内有效。"
        confirmLabel="提交请求"
        busy={busy}
        confirmDisabled={dualReason.trim().length < 2}
        onConfirm={() => void submitDualRequest()}
        onClose={() => {
          setDualTarget(null);
          setDualReason('');
        }}
      >
        <div className="grid gap-2">
          <Label htmlFor="dual-reason">原因（至少 2 个字）</Label>
          <Textarea id="dual-reason" rows={2} value={dualReason} onChange={(e) => setDualReason(e.target.value)} />
        </div>
      </ConfirmDialog>

      <ConfirmDialog
        open={target !== null}
        title={target ? '中断 ' + target.id : '中断会话'}
        description="提交后服务端会关闭会话并向双方传播结束事件；下方展示的是服务端返回的传播与审计结果，不是前端推断。"
        confirmLabel="确认中断"
        danger
        busy={busy}
        confirmDisabled={!reasonValid}
        onConfirm={() => void kill()}
        onClose={() => {
          setTarget(null);
          setCategory('');
          setDetail('');
        }}
      >
        <div className="grid gap-3">
          <div className="grid gap-2">
            <Label htmlFor="kill-category">原因类别</Label>
            <Select
              value={category === '' ? SENTINEL : category}
              onValueChange={(v) => setCategory(v === SENTINEL ? '' : v)}
            >
              <SelectTrigger id="kill-category" aria-label="kill-category" className={cn('w-[170px]', 'h-9 rounded-md border border-input bg-background px-3 text-sm')}><SelectValue /></SelectTrigger>
              <SelectContent>
              <SelectItem value={SENTINEL} data-value={SENTINEL}>请选择原因类别</SelectItem>
              {KILL_REASON_CATEGORIES.map((c) => (
                <SelectItem key={c.value} value={c.value} data-value={c.value}>
                  {c.label}
                </SelectItem>
              ))}
            </SelectContent></Select>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="kill-detail">补充说明（可选）</Label>
            <Input
              id="kill-detail"
              value={detail}
              onChange={(e) => setDetail(e.target.value)}
              placeholder="例如：用户反馈画面卡死"
            />
          </div>
          <p className="text-xs text-muted-foreground">
            写入审计的原因：{reason || '（未填写）'}
          </p>
        </div>
      </ConfirmDialog>
    </div>
  );
}

/* ---------- 审计 ---------- */

function Audit() {
  const [rows, setRows] = useState<AuditRow[]>([]);
  const [total, setTotal] = useState(0);
  const [query, setQuery] = useState('');
  const [action, setAction] = useState('');
  const [result, setResult] = useState('all');
  const [actor, setActor] = useState('');
  const [object, setObject] = useState('');
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');
  const [actionOptions, setActionOptions] = useState<string[]>([]);
  const [page, setPage] = useState(1);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [exporting, setExporting] = useState(false);
  const [exportInfo, setExportInfo] = useState('');

  async function exportCsv() {
    setExporting(true);
    setError(null);
    try {
      const res = await api.exportAudit({
        action: normalizeActionFilter(action),
        result: result === 'all' ? undefined : result,
        actor: actor.trim() || undefined,
        object: object.trim() || undefined,
        from: from ? new Date(from).toISOString() : undefined,
        to: to ? new Date(to).toISOString() : undefined,
      });
      const blob = new Blob(['\uFEFF' + res.csv], { type: 'text/csv;charset=utf-8' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = 'audit-export.csv';
      a.click();
      URL.revokeObjectURL(url);
      setError(null);
      setExportInfo('已导出 ' + res.rows + ' 行' + (res.truncated ? '（达到上限，已截断）' : ''));
    } catch (err) {
      setError(err);
    } finally {
      setExporting(false);
    }
  }

  const load = useCallback(async () => {
    try {
      setLoading(true);
      const offset = (page - 1) * PAGE_SIZE;
      const res = await api.audit({
        limit: PAGE_SIZE,
        offset,
        q: query.trim() || undefined,
        action: normalizeActionFilter(action),
        result: result === 'all' ? undefined : result,
        actor: actor.trim() || undefined,
        object: object.trim() || undefined,
        from: from ? new Date(from).toISOString() : undefined,
        to: to ? new Date(to).toISOString() : undefined,
      });
      setRows(res.items);
      setTotal(res.total);
      setError(null);
    } catch (err) {
      setError(err);
    } finally {
      setLoading(false);
    }
  }, [page, query, action, result, actor, object, from, to]);

  // 合法动作集合来自服务端（ADM-API-05a）；失败时保留空列表，不猜。
  useEffect(() => {
    let alive = true;
    api
      .auditActions()
      .then((res) => {
        if (alive) setActionOptions(res.actions);
      })
      .catch(() => {
        if (alive) setActionOptions([]);
      });
    return () => {
      alive = false;
    };
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  // 动作选项来自服务端 /admin/v1/audit/actions；本页数据不再参与构造下拉。


  return (
    <div className="stack">
      <ErrorState error={error} />
      {exportInfo ? <div className="banner ok"><span>{exportInfo}</span></div> : null}
      <Card
        title={'审计日志 (' + total + ')'}
        subtitle="append-only · 记录操作人、对象、IP 与结果"
        actions={
          <>
            <Input
              placeholder="搜索操作人 / 对象 / IP"
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
                setPage(1);
              }}
            />
            <Select
              value={action === '' ? SENTINEL : action}
              onValueChange={(v) => {
                setAction(v === SENTINEL ? '' : v);
                setPage(1);
              }}
            >
              <SelectTrigger aria-label="动作筛选" className="w-[170px]"><SelectValue /></SelectTrigger>
              <SelectContent>
              <SelectItem value={SENTINEL} data-value={SENTINEL}>全部动作</SelectItem>
              {actionOptions.map((a) => (
                <SelectItem key={a} value={a} data-value={a}>
                  {a}
                </SelectItem>
              ))}
            </SelectContent></Select>
            <Input
              aria-label="操作人筛选"
              placeholder="操作人（精确）"
              value={actor}
              onChange={(e) => {
                setActor(e.target.value);
                setPage(1);
              }}
            />
            <Input
              aria-label="对象筛选"
              placeholder="对象（精确）"
              value={object}
              onChange={(e) => {
                setObject(e.target.value);
                setPage(1);
              }}
            />
            <Input
              type="datetime-local"
              aria-label="开始时间"
              value={from}
              onChange={(e) => {
                setFrom(e.target.value);
                setPage(1);
              }}
            />
            <Input
              type="datetime-local"
              aria-label="结束时间"
              value={to}
              onChange={(e) => {
                setTo(e.target.value);
                setPage(1);
              }}
            />
            <button
              className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))}
              disabled={exporting}
              title="按当前筛选导出 CSV（服务端脱敏 + 写审计）"
              onClick={() => void exportCsv()}
            >
              {exporting ? '导出中…' : '导出 CSV'}
            </button>
            <Select value={result} onValueChange={(v) => { setResult(v); setPage(1); }}>
              <SelectTrigger aria-label="结果筛选" className="w-[170px]"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="all" data-value="all">全部结果</SelectItem>
                <SelectItem value="ok" data-value="ok">ok</SelectItem>
                <SelectItem value="denied" data-value="denied">denied</SelectItem>
              </SelectContent>
            </Select>
            <button className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))} onClick={() => void load()}>
              刷新
            </button>
          </>
        }
      >
        {loading ? (
          <LoadingState />
        ) : (
          <DataTable
            ariaLabel="审计日志"
            rows={rows}
            rowKey={(a) => a.id}
            empty={<EmptyState text="没有匹配的审计记录" />}
            columns={[
              { key: 'ts', header: '时间', className: 'muted nowrap', render: (a) => fmtTime(a.ts) },
              { key: 'actor', header: '操作人', render: (a) => a.actor },
              { key: 'action', header: '动作', className: 'mono', render: (a) => a.action },
              { key: 'object', header: '对象', className: 'mono', render: (a) => a.object },
              {
                key: 'result',
                header: '结果',
                render: (a) => <Pill tone={a.result === 'ok' ? 'ok' : 'danger'}>{a.result}</Pill>,
              },
              { key: 'ip', header: 'IP', className: 'mono muted', render: (a) => a.ip },
              {
                key: 'diff',
                header: '变更',
                className: 'diff',
                render: (a) =>
                  a.before || a.after ? (
                    <>
                      {a.before && <span className="before">{a.before}</span>}
                      {a.before && a.after && <span className="arrow">→</span>}
                      {a.after && <span className="after">{a.after}</span>}
                    </>
                  ) : (
                    <span className="muted">—</span>
                  ),
              },
            ]}
          />
        )}
        <Pagination page={page} total={total} onChange={setPage} />
      </Card>
    </div>
  );
}

/* ---------- 配置中心（P1-1） ---------- */

function ConfigCenter({ notice }: { notice: (t: string) => void }) {
  const canWrite = useCan('config.write');
  const [view, setView] = useState<ConfigView | null>(null);
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState('');
  const [history, setHistory] = useState<ConfigHistoryEntry[]>([]);
  const [platform, setPlatform] = useState('');
  const [version, setVersion] = useState('');

  const load = useCallback(async () => {
    try {
      const [v, h] = await Promise.all([api.config(platform, version), api.configHistory(20)]);
      setView(v);
      setDraft(v.config);
      setHistory(h.items);
      setError(null);
    } catch (err) {
      setError(err);
    } finally {
      setLoading(false);
    }
  }, [platform, version]);

  useEffect(() => {
    void load();
  }, [load]);

  const fields: ConfigField[] = view?.fields ?? [];
  const changes = changedFields(fields, draft, view?.config ?? {});
  const invalid = invalidFields(fields, draft);
  const saveAllowed = canSave({ canWrite, busy, changes, invalid });

  async function rollback(key: string, value: string) {
    setBusy(true);
    setError(null);
    try {
      await api.rollbackConfig(key, value);
      notice('已回滚 ' + key + ' → ' + value);
      await load();
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }

  async function save() {
    if (!saveAllowed) return;
    setBusy(true);
    setError(null);
    setSaved('');
    try {
      const values: Record<string, string> = {};
      for (const c of changes) values[c.key] = (draft[c.key] ?? '').trim();
      const res = await api.saveConfig(values, platform, version);
      setSaved('已保存 ' + changes.length + ' 项' + (res.audited ? '，变更已写入审计（before / after）' : ''));
      notice('配置已保存：' + changes.map((c) => c.key).join('、'));
      await load();
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="stack">
      <ErrorState error={error} onClose={() => setError(null)} onRetry={() => void load()} />
      {saved && <div className="banner ok"><span>{saved}</span></div>}
      <Card
        title="待提交变更"
        subtitle={changes.length === 0 ? '没有改动' : '保存前确认 diff；保存会写审计（before / after）'}
      >
        {changes.length === 0 ? (
          <EmptyState text="修改任意配置项后，这里会列出 before → after。" />
        ) : (
          <ul className="grid gap-1 text-sm">
            {changes.map((c) => (
              <li key={c.key} className="font-mono">
                <span className="text-muted-foreground">{c.key}</span>：{c.from || '（空）'} → {c.to || '（空）'}
              </li>
            ))}
          </ul>
        )}
        {invalid.length > 0 ? (
          <p className="mt-2 text-xs text-destructive">不合法：{invalid.map((f) => f.key).join('、')}</p>
        ) : null}
      </Card>

      <Card
        title="配置中心"
        subtitle={
          (platform === ''
            ? '作用域：全局'
            : '作用域：' + platform + (version ? ' ' + version : '（任意版本）') + '（优先于全局）') +
          (canWrite ? ' · 保存即下发，写操作进审计' : ' · 只读：当前角色无配置写权限')
        }
        actions={
          <>
            <Input
              aria-label="配置版本作用域"
              placeholder="版本（可选，如 0.2.13）"
              value={version}
              onChange={(e) => setVersion(e.target.value)}
            />
            <Select
              value={platform === '' ? SENTINEL : platform}
              onValueChange={(v) => setPlatform(v === SENTINEL ? '' : v)}
            >
              <SelectTrigger aria-label="配置作用域" className="w-[170px]"><SelectValue /></SelectTrigger>
              <SelectContent>
              <SelectItem value={SENTINEL} data-value={SENTINEL}>全局</SelectItem>
              <SelectItem value="windows" data-value="windows">windows</SelectItem>
              <SelectItem value="macos" data-value="macos">macos</SelectItem>
              <SelectItem value="android" data-value="android">android</SelectItem>
              <SelectItem value="ios" data-value="ios">ios</SelectItem>
            </SelectContent></Select>
            <button className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))} onClick={() => void load()} disabled={busy}>
              重新加载
            </button>
            <button
              className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))}
              onClick={() => view && setDraft(view.config)}
              disabled={busy || changes.length === 0}
            >
              撤销修改
            </button>
            <button className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))} onClick={() => void load()} disabled={busy}>
              刷新历史
            </button>
            <button className={cn(buttonVariants({ variant: 'secondary', size: 'sm' }))} onClick={() => void save()} disabled={!saveAllowed}>
              {busy ? '保存中…' : '保存 ' + (changes.length > 0 ? changes.length + ' 项' : '')}
            </button>
          </>
        }
      >
        {loading ? (
          <LoadingState />
        ) : fields.length === 0 ? (
          <EmptyState text="服务端没有返回可配置项" />
        ) : (
          <Table aria-label="配置项" data-testid="data-table">
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead>配置项</TableHead>
                <TableHead>当前值</TableHead>
                <TableHead>默认 / 范围</TableHead>
                <TableHead>说明</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {fields.map((f) => {
                const raw = draft[f.key] ?? '';
                const bad =
                  raw.trim() === '' ||
                  ((f.kind === 'int' || f.kind === 'int64') && !Number.isFinite(Number(raw))) ||
                  ((f.kind === 'int' || f.kind === 'int64') &&
                    ((f.min !== undefined && Number(raw) < f.min) || (f.max !== undefined && Number(raw) > f.max)));
                return (
                  <TableRow key={f.key}>
                    <TableCell className="whitespace-nowrap font-mono text-xs">
                      {f.group ? <span className="text-muted-foreground">{f.group} · </span> : null}
                      {f.key}
                    </TableCell>
                    <TableCell>
                      {f.kind === 'bool' ? (
                        <Select value={raw} onValueChange={(v) => setDraft({ ...draft, [f.key]: v })}>
                          <SelectTrigger aria-label={f.key} className="w-[120px]"><SelectValue /></SelectTrigger>
                          <SelectContent>
                            <SelectItem value="true" data-value="true">true</SelectItem>
                            <SelectItem value="false" data-value="false">false</SelectItem>
                          </SelectContent>
                        </Select>
                      ) : (
                        <input
                          disabled={!canWrite}
                          className={'cfg-input' + (bad ? ' bad' : '')}
                          value={raw}
                          inputMode={f.kind === 'int' || f.kind === 'int64' ? 'numeric' : 'text'}
                          onChange={(e) => setDraft({ ...draft, [f.key]: e.target.value })}
                        />
                      )}
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-muted-foreground">
                      {f.default}
                      {f.min !== undefined || f.max !== undefined ? ' · ' + (f.min ?? '—') + ' ~ ' + (f.max ?? '—') : ''}
                    </TableCell>
                    <TableCell className="text-muted-foreground">{f.help}</TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        )}
        {invalid.length > 0 && (
          <p className="muted" style={{ marginTop: 10 }}>
            以下配置项取值不合法：{invalid.map((f) => f.key).join('、')}
          </p>
        )}
      </Card>
      <Card title="变更历史" subtitle="来自审计 config.update / config.rollback · 回滚会写审计（ADM-API-05b）">
        {history.length === 0 ? (
          <EmptyState text="还没有配置变更记录。" />
        ) : (
          <ul className="grid gap-3 text-sm">
            {history.map((h) => {
              const before = parseKV(h.before);
              const after = parseKV(h.after);
              const keys = [...new Set([...Object.keys(before), ...Object.keys(after)])];
              return (
                <li key={h.id} className="grid gap-1 border-b border-border pb-2 last:border-0">
                  <div className="text-xs text-muted-foreground">
                    {fmtTime(h.ts)} · {h.actor} · {h.action} · {h.result}
                  </div>
                  {keys.length === 0 ? (
                    <div className="text-xs text-muted-foreground">（无可解析的键值）</div>
                  ) : (
                    keys.map((k) => (
                      <div key={k} className="flex flex-wrap items-center gap-2 font-mono text-xs">
                        <span>{k}</span>
                        <span className="muted">{before[k] ?? '—'}</span>
                        <span>→</span>
                        <span>{after[k] ?? '—'}</span>
                        {canWrite && before[k] !== undefined && before[k] !== after[k] ? (
                          <button
                            className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))}
                            disabled={busy}
                            onClick={() => void rollback(k, before[k])}
                          >
                            回滚到 {before[k]}
                          </button>
                        ) : null}
                      </div>
                    ))
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </Card>

      <Card
        title="能力成熟度（只读）"
        subtitle="来自 docs/spec/capabilities.yaml 生成物；服务端只能再关闭，不能在运行时解锁未成熟能力"
      >
        <Table aria-label="能力成熟度">
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead>能力</TableHead>
              <TableHead>成熟度</TableHead>
              <TableHead>收口卡</TableHead>
              <TableHead>为什么还没有开放</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {CAPABILITIES.map((c) => (
              <TableRow key={c.key}>
                <TableCell>
                  <div className="text-[13px]">{c.title}</div>
                  <div className="font-mono text-xs text-muted-foreground">{c.key}</div>
                </TableCell>
                <TableCell className={c.maturity === 'stable' ? 'text-[13px]' : 'text-[13px] text-muted-foreground'}>
                  {c.maturity}
                </TableCell>
                <TableCell className="whitespace-nowrap font-mono text-xs">{c.owner}</TableCell>
                <TableCell className="text-[13px] text-muted-foreground">{c.reason}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        <p className="muted" style={{ marginTop: 10 }}>
          disabled 能力任何构建都不可用；experimental 只在开发构建可见，发布构建隐藏入口。这里不提供开关。
        </p>
      </Card>
    </div>
  );
}

/* ---------- 详情（P1-2） ---------- */

/* ---------- 主框架 ---------- */

/* ---------- 路由守卫与页面容器 ---------- */

function Forbidden() {
  return (
    <Card title="无权限" subtitle="当前角色没有该页面的读取权限">
      <EmptyState text="请由超级管理员授予对应权限；服务端仍会独立校验，前端隐藏不等于授权。" />
    </Card>
  );
}

const ADMIN_ROLES = ['super', 'ops', 'cs', 'risk', 'readonly'] as const;

/** A14 管理员与角色：只对超管可见（路由权限 admin.read）。 */
function Admins({ notice }: { notice: (t: string) => void }) {
  const [rows, setRows] = useState<AdminRow[]>([]);
  const [total, setTotal] = useState(0);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [pending, setPending] = useState<{ admin: AdminRow; patch: { role?: string; status?: string } } | null>(
    null,
  );
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      setLoading(true);
      const res = await api.admins({ limit: 50 });
      setRows(res.items);
      setTotal(res.total);
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

  async function submit() {
    if (!pending || reason.trim().length < 2) return;
    setBusy(true);
    setError(null);
    try {
      await api.updateAdmin(pending.admin.id, { ...pending.patch, reason: reason.trim() });
      notice('已更新 ' + pending.admin.username);
      setPending(null);
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
      <ErrorState error={error} />
      <Card
        title={'管理员 (' + total + ')'}
        subtitle="改角色 / 启用禁用都会写审计；不能移除最后一个可用超管"
        actions={
          <button className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))} onClick={() => void load()} disabled={loading}>
            刷新
          </button>
        }
      >
        {loading ? (
          <LoadingState />
        ) : rows.length === 0 ? (
          <EmptyState text="没有可显示的管理员。" />
        ) : (
          <DataTable
            ariaLabel="管理员列表"
            rows={rows}
            rowKey={(a) => a.id}
            columns={[
              { key: 'username', header: '用户名', render: (a) => a.username },
              {
                key: 'role',
                header: '角色',
                render: (a) => (
                  <Select
                    value={a.role}
                    onValueChange={(v) => {
                      setPending({ admin: a, patch: { role: v } });
                      setReason('');
                    }}
                  >
                    <SelectTrigger aria-label={'角色 ' + a.username} className="w-[140px]"><SelectValue /></SelectTrigger>
                    <SelectContent>
                    {ADMIN_ROLES.map((r) => (
                      <SelectItem key={r} value={r} data-value={r}>
                        {r}
                      </SelectItem>
                    ))}
                  </SelectContent></Select>
                ),
              },
              {
                key: 'status',
                header: '状态',
                render: (a) => (
                  <Pill tone={a.status === 'active' ? 'ok' : 'muted'}>
                    {a.status === 'active' ? '启用' : '禁用'}
                  </Pill>
                ),
              },
              {
                key: 'mfa',
                header: 'MFA',
                render: (a) =>
                  a.mfa_enabled ? <Pill tone="ok">已开启</Pill> : <Pill tone="warn">未开启</Pill>,
              },
              {
                key: 'actions',
                header: '操作',
                render: (a) => (
                  <button
                    className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))}
                    onClick={() => {
                      setPending({
                        admin: a,
                        patch: { status: a.status === 'active' ? 'disabled' : 'active' },
                      });
                      setReason('');
                    }}
                  >
                    {a.status === 'active' ? '禁用' : '启用'}
                  </button>
                ),
              },
            ]}
          />
        )}
      </Card>

      <ConfirmDialog
        open={pending !== null}
        title={pending ? '修改 ' + pending.admin.username : '修改管理员'}
        description={
          pending?.patch.role
            ? '把角色改为 ' + pending.patch.role + '，操作写入审计。'
            : '改变启用状态，操作写入审计。'
        }
        confirmLabel="确认修改"
        danger={pending?.patch.status === 'disabled'}
        busy={busy}
        confirmDisabled={reason.trim().length < 2}
        onConfirm={() => void submit()}
        onClose={() => {
          setPending(null);
          setReason('');
        }}
      >
        <div className="grid gap-2">
          <Label htmlFor="admin-reason">原因（至少 2 个字）</Label>
          <Textarea id="admin-reason" rows={2} value={reason} onChange={(e) => setReason(e.target.value)} />
        </div>
      </ConfirmDialog>
    </div>
  );
}

const BLACKLIST_KINDS = [
  { value: 'account', label: '账号 ID' },
  { value: 'device', label: '设备码' },
  { value: 'ip', label: 'IP / CIDR' },
] as const;

/** A10 风控中心：黑名单（账号/设备/IP+CIDR）与异常连接告警。 */
function Risk() {
  const [entries, setEntries] = useState<BlacklistRow[]>([]);
  const [alerts, setAlerts] = useState<RiskAlertRow[]>([]);
  const [total, setTotal] = useState(0);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<BlacklistRow | null>(null);
  const [busy, setBusy] = useState(false);
  const [kind, setKind] = useState<string>('account');
  const [value, setValue] = useState('');
  const [note, setNote] = useState('');
  const [ttl, setTtl] = useState('');
  const [reason, setReason] = useState('');

  const load = useCallback(async () => {
    try {
      setLoading(true);
      const [bl, al] = await Promise.all([api.riskBlacklist({ limit: 50 }), api.riskAlerts({ limit: 50 })]);
      setEntries(bl.items);
      setTotal(bl.total);
      setAlerts(al.items);
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

  async function submitAdd() {
    if (value.trim() === '') return;
    setBusy(true);
    setError(null);
    try {
      await api.addBlacklist({
        kind,
        value: value.trim(),
        note: note.trim(),
        ttl_hours: ttl.trim() === '' ? 0 : Number(ttl),
      });
      setAdding(false);
      setValue('');
      setNote('');
      setTtl('');
      await load();
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }

  async function submitRemove() {
    if (!removing || reason.trim().length < 2) return;
    setBusy(true);
    setError(null);
    try {
      await api.removeBlacklist(removing.id, reason.trim());
      setRemoving(null);
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
      <ErrorState error={error} />
      <Card
        title={'黑名单 (' + total + ')'}
        subtitle="命中黑名单的连接会被拒绝并记入告警；IP 支持 CIDR，有效期到期后自动失效"
        actions={
          <button className={cn(buttonVariants({ variant: 'secondary', size: 'sm' }))} onClick={() => setAdding(true)}>
            新增黑名单
          </button>
        }
      >
        {loading ? (
          <LoadingState />
        ) : entries.length === 0 ? (
          <EmptyState text="黑名单为空。" hint="新增后立即对连接生效（无需重启）。" />
        ) : (
          <DataTable
            ariaLabel="黑名单列表"
            rows={entries}
            rowKey={(e) => e.id}
            columns={[
              { key: 'kind', header: '类型', render: (e) => e.kind },
              { key: 'value', header: '值', className: 'mono', render: (e) => e.value },
              { key: 'note', header: '备注', className: 'muted', render: (e) => e.note || '—' },
              {
                key: 'expires',
                header: '到期',
                render: (e) => (e.expires_at ? fmtTime(e.expires_at) : '永久'),
              },
              { key: 'by', header: '创建人', className: 'muted', render: (e) => e.created_by || '—' },
              {
                key: 'actions',
                header: '操作',
                render: (e) => (
                  <button
                    className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))}
                    onClick={() => {
                      setRemoving(e);
                      setReason('');
                    }}
                  >
                    移除
                  </button>
                ),
              },
            ]}
          />
        )}
      </Card>

      <Card title="异常连接告警" subtitle="命中黑名单的连接尝试会在这里留痕（A10）">
        {alerts.length === 0 ? (
          <EmptyState text="还没有告警。" hint="有连接被黑名单拦截时会出现记录。" />
        ) : (
          <DataTable
            ariaLabel="风控告警"
            rows={alerts}
            rowKey={(a) => a.id}
            columns={[
              { key: 'ts', header: '时间', className: 'muted', render: (a) => fmtTime(a.ts) },
              { key: 'kind', header: '类型', render: (a) => a.kind },
              { key: 'subject', header: '主体', className: 'mono', render: (a) => a.subject },
              { key: 'detail', header: '详情', className: 'muted', render: (a) => a.detail || '—' },
            ]}
          />
        )}
      </Card>

      <ConfirmDialog
        open={adding}
        title="新增黑名单"
        description="保存后立即生效；操作写入审计。"
        confirmLabel="确认新增"
        busy={busy}
        confirmDisabled={value.trim() === ''}
        onConfirm={() => void submitAdd()}
        onClose={() => setAdding(false)}
      >
        <div className="grid gap-3">
          <div className="grid gap-1">
            <Label htmlFor="risk-kind">类型</Label>
            <Select value={kind} onValueChange={(v) => setKind(v)}>
              <SelectTrigger aria-label="风控类型" className="w-full"><SelectValue /></SelectTrigger>
              <SelectContent>
                {BLACKLIST_KINDS.map((k) => (
                  <SelectItem key={k.value} value={k.value} data-value={k.value}>
                    {k.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-1">
            <Label htmlFor="risk-value">值</Label>
            <Input id="risk-value" value={value} onChange={(e) => setValue(e.target.value)} placeholder="账号 ID / 设备码 / 1.2.3.4 或 10.0.0.0/8" />
          </div>
          <div className="grid gap-1">
            <Label htmlFor="risk-note">备注</Label>
            <Input id="risk-note" value={note} onChange={(e) => setNote(e.target.value)} placeholder="例如：同 IP 高频连接" />
          </div>
          <div className="grid gap-1">
            <Label htmlFor="risk-ttl">有效期（小时，留空=永久）</Label>
            <Input id="risk-ttl" value={ttl} onChange={(e) => setTtl(e.target.value)} inputMode="numeric" />
          </div>
        </div>
      </ConfirmDialog>

      <ConfirmDialog
        open={removing !== null}
        title={removing ? '移除 ' + removing.value : '移除黑名单'}
        description="移除后连接立即恢复；操作写入审计。"
        confirmLabel="确认移除"
        danger
        busy={busy}
        confirmDisabled={reason.trim().length < 2}
        onConfirm={() => void submitRemove()}
        onClose={() => {
          setRemoving(null);
          setReason('');
        }}
      >
        <div className="grid gap-2">
          <Label htmlFor="risk-reason">原因（至少 2 个字）</Label>
          <Textarea id="risk-reason" rows={2} value={reason} onChange={(e) => setReason(e.target.value)} />
        </div>
      </ConfirmDialog>
    </div>
  );
}

const PLATFORMS = ['windows', 'macos', 'android', 'ios'] as const;
const VERSION_STATUSES = [
  { value: 'draft', label: '草稿' },
  { value: 'rolling', label: '灰度中' },
  { value: 'full', label: '全量' },
  { value: 'retired', label: '已下线' },
] as const;

/** 与后端 validateRollout 对齐的前端预校验（服务端仍是唯一权威）。 */
function rolloutError(status: string, percent: number): string | null {
  if (status === 'draft' && percent !== 0) return '草稿版本覆盖率必须为 0';
  if (status === 'rolling' && (percent < 1 || percent > 99)) return '灰度覆盖率必须在 1-99';
  if (status === 'full' && percent !== 100) return '全量版本覆盖率必须为 100';
  return null;
}

/** A11 版本管理：发布/灰度/强制升级/下线。 */
function Versions() {
  const [rows, setRows] = useState<ClientVersionRow[]>([]);
  const [total, setTotal] = useState(0);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [publishing, setPublishing] = useState(false);
  const [retiring, setRetiring] = useState<ClientVersionRow | null>(null);
  const [busy, setBusy] = useState(false);
  const [platform, setPlatform] = useState<string>('windows');
  const [version, setVersion] = useState('');
  const [status, setStatus] = useState<string>('draft');
  const [percent, setPercent] = useState('0');
  const [force, setForce] = useState(false);
  const [notes, setNotes] = useState('');
  const [reason, setReason] = useState('');

  const load = useCallback(async () => {
    try {
      setLoading(true);
      const res = await api.versions({ limit: 50 });
      setRows(res.items);
      setTotal(res.total);
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

  const percentNum = Number(percent === '' ? '0' : percent);
  const localError = rolloutError(status, Number.isFinite(percentNum) ? percentNum : -1);

  async function submitPublish() {
    if (version.trim() === '' || localError) return;
    setBusy(true);
    setError(null);
    try {
      await api.publishVersion({
        platform,
        version: version.trim(),
        status,
        rollout_percent: percentNum,
        force_update: force,
        notes: notes.trim(),
      });
      setPublishing(false);
      setVersion('');
      setNotes('');
      setPercent('0');
      setStatus('draft');
      setForce(false);
      await load();
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }

  async function submitRetire() {
    if (!retiring || reason.trim().length < 2) return;
    setBusy(true);
    setError(null);
    try {
      await api.retireVersion(retiring.id, reason.trim());
      setRetiring(null);
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
      <ErrorState error={error} />
      <Card
        title={'客户端版本 (' + total + ')'}
        subtitle="灰度覆盖率与状态必须一致（草稿 0 / 灰度 1-99 / 全量 100）；下线需填原因"
        actions={
          <button className={cn(buttonVariants({ variant: 'secondary', size: 'sm' }))} onClick={() => setPublishing(true)}>
            发布版本
          </button>
        }
      >
        {loading ? (
          <LoadingState />
        ) : rows.length === 0 ? (
          <EmptyState text="还没有发布过客户端版本。" hint="发布后客户端才能按版本/覆盖率升级。" />
        ) : (
          <DataTable
            ariaLabel="版本列表"
            rows={rows}
            rowKey={(v) => v.id}
            columns={[
              { key: 'platform', header: '平台', render: (v) => v.platform },
              { key: 'version', header: '版本号', className: 'mono', render: (v) => v.version },
              {
                key: 'status',
                header: '状态',
                render: (v) => (
                  <Pill tone={v.status === 'full' ? 'ok' : v.status === 'retired' ? 'muted' : 'warn'}>
                    {VERSION_STATUSES.find((s) => s.value === v.status)?.label ?? v.status}
                  </Pill>
                ),
              },
              { key: 'rollout', header: '覆盖率', render: (v) => v.rollout_percent + '%' },
              {
                key: 'force',
                header: '强制升级',
                render: (v) => (v.force_update ? <Pill tone="danger">是</Pill> : <span className="muted">否</span>),
              },
              { key: 'notes', header: '更新日志', className: 'muted', render: (v) => v.notes || '—' },
              { key: 'updated', header: '更新时间', className: 'muted', render: (v) => fmtTime(v.updated_at) },
              {
                key: 'actions',
                header: '操作',
                render: (v) =>
                  v.status === 'retired' ? (
                    <span className="muted text-xs">已下线</span>
                  ) : (
                    <button
                      className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))}
                      onClick={() => {
                        setRetiring(v);
                        setReason('');
                      }}
                    >
                      下线
                    </button>
                  ),
              },
            ]}
          />
        )}
      </Card>

      <ConfirmDialog
        open={publishing}
        title="发布客户端版本"
        description="同一平台+版本号重复发布会更新策略字段（不新增行）。"
        confirmLabel="确认发布"
        busy={busy}
        confirmDisabled={version.trim() === '' || localError !== null}
        onConfirm={() => void submitPublish()}
        onClose={() => setPublishing(false)}
      >
        <div className="grid gap-3">
          <div className="grid gap-1">
            <Label htmlFor="ver-platform">平台</Label>
            <Select value={platform} onValueChange={(v) => setPlatform(v)}>
              <SelectTrigger id="ver-platform" aria-label="ver-platform" className="w-full"><SelectValue /></SelectTrigger>
              <SelectContent>
                {PLATFORMS.map((p) => (
                  <SelectItem key={p} value={p} data-value={p}>
                    {p}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-1">
            <Label htmlFor="ver-version">版本号</Label>
            <Input id="ver-version" value={version} onChange={(e) => setVersion(e.target.value)} placeholder="0.2.14" />
          </div>
          <div className="grid gap-1">
            <Label htmlFor="ver-status">状态</Label>
            <Select
              value={status}
              onValueChange={(v) => {
                const next = v;
                setStatus(next);
                setPercent(next === 'full' ? '100' : next === 'draft' ? '0' : '20');
              }}
            >
              <SelectTrigger id="ver-status" aria-label="ver-status" className="w-full"><SelectValue /></SelectTrigger>
              <SelectContent>
                {VERSION_STATUSES.filter((s) => s.value !== 'retired').map((s) => (
                  <SelectItem key={s.value} value={s.value} data-value={s.value}>
                    {s.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-1">
            <Label htmlFor="ver-percent">覆盖率 (%)</Label>
            <Input id="ver-percent" value={percent} onChange={(e) => setPercent(e.target.value)} inputMode="numeric" />
            {localError ? <p className="text-xs text-destructive">{localError}</p> : null}
          </div>
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={force} onChange={(e) => setForce(e.target.checked)} /> 低于此版本强制升级
          </label>
          <div className="grid gap-1">
            <Label htmlFor="ver-notes">更新日志</Label>
            <Textarea id="ver-notes" rows={2} value={notes} onChange={(e) => setNotes(e.target.value)} />
          </div>
        </div>
      </ConfirmDialog>

      <ConfirmDialog
        open={retiring !== null}
        title={retiring ? '下线 ' + retiring.platform + ' ' + retiring.version : '下线版本'}
        description="下线后该版本不再投放；操作写入审计。"
        confirmLabel="确认下线"
        danger
        busy={busy}
        confirmDisabled={reason.trim().length < 2}
        onConfirm={() => void submitRetire()}
        onClose={() => {
          setRetiring(null);
          setReason('');
        }}
      >
        <div className="grid gap-2">
          <Label htmlFor="ver-reason">原因（至少 2 个字）</Label>
          <Textarea id="ver-reason" rows={2} value={reason} onChange={(e) => setReason(e.target.value)} />
        </div>
      </ConfirmDialog>
    </div>
  );
}

function NotFound() {
  return (
    <Card title="页面不存在" subtitle="链接可能已失效">
      <EmptyState text="请从左侧导航进入需要的页面。" />
    </Card>
  );
}

function Shell({
  identity,
  notice,
  noticeText,
  onCloseNotice,
  onRefresh,
  onSignedOut,
}: {
  identity: AdminIdentity;
  notice: (t: string) => void;
  noticeText: string;
  onCloseNotice: () => void;
  onRefresh: () => void;
  onSignedOut: () => void;
}) {
  const location = useLocation();
  const navigate = useNavigate();
  const resolution = resolvePath(location.pathname, identity.permissions);
  const navRoutes = visibleRoutes(identity.permissions);
  const title =
    resolution.kind === 'page'
      ? resolution.route.label
      : resolution.kind === 'forbidden'
        ? '无权限'
        : resolution.kind === 'notfound'
          ? '页面不存在'
          : '仪表盘';

  const signOut = async () => {
    try {
      await api.logout();
    } catch (err) {
      notice('服务端登出失败：' + errorMeta(err).message);
    }
    setToken(null);
    setIdentity(null);
    onSignedOut();
    navigate(LOGIN_PATH, { replace: true });
  };

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
          {navRoutes.map((r) => (
            <NavLink
              key={r.id}
              to={r.path}
              className={({ isActive }) => 'nav' + (isActive ? ' active' : '')}
            >
              <Icon name={NAV_ICONS[r.id]} className="nav-ico" />
              {r.label}
            </NavLink>
          ))}
        </nav>
        <div className="side-foot">
          <div className="who">
            <span className="avatar">{identity.username.slice(0, 1).toUpperCase()}</span>
            <div>
              <strong>{identity.username}</strong>
              <span>{ROLE_LABEL[identity.role] ?? identity.role}</span>
            </div>
          </div>
          <button className={cn(buttonVariants({ variant: 'link' }), 'h-auto min-h-6 p-0 text-xs')} onClick={() => void signOut()}>
            退出登录
          </button>
        </div>
      </aside>

      <main className="main">
        <header className="topbar">
          <div>
            <h2>{title}</h2>
            <p>所有写操作按角色权限执行并写入审计</p>
          </div>
          <div className="top-actions">
            <Button variant="outline" size="sm" onClick={onRefresh}>
              <Icon name="refresh" className="mr-1.5 h-3.5 w-3.5" />
              刷新数据
            </Button>
          </div>
        </header>
        {noticeText && <InfoBanner text={noticeText} onClose={onCloseNotice} />}
        <Outlet />
      </main>
    </div>
  );
}

export default function App() {
  const [identity, setIdentityState] = useState<AdminIdentity | null>(() => getIdentity());
  const [noticeText, setNoticeText] = useState('');
  const [refreshKey, setRefreshKey] = useState(0);
  const timer = useRef<number | null>(null);
  const navigate = useNavigate();

  const notice = useCallback((t: string) => {
    setNoticeText(t);
    if (timer.current) window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setNoticeText(''), 4000);
  }, []);

  const permissions = identity?.permissions ?? [];
  const configAllowed = canAccess(routeById('config'), permissions);

  return (
    <AppProviders permissions={permissions}>
      <Routes>
        <Route
          path={LOGIN_PATH}
          element={
            identity ? (
              <Navigate to={HOME_PATH} replace />
            ) : (
              <Login
                onDone={() => {
                  setIdentityState(getIdentity());
                  navigate(HOME_PATH, { replace: true });
                }}
              />
            )
          }
        />
        <Route
          element={
            identity ? (
              <Shell
                identity={identity}
                notice={notice}
                noticeText={noticeText}
                onCloseNotice={() => setNoticeText('')}
                onRefresh={() => setRefreshKey((k) => k + 1)}
                onSignedOut={() => setIdentityState(null)}
              />
            ) : (
              <Navigate to={LOGIN_PATH} replace />
            )
          }
        >
          <Route index element={<Navigate to={HOME_PATH} replace />} />
          <Route
            path={childPath(routeById('dashboard'))}
            element={<Dashboard onOpen={(p) => navigate(routeById(p).path)} refreshKey={refreshKey} />}
          />
          <Route path={childPath(routeById('users'))} element={<Users notice={notice} />} />
          <Route path={childPath(routeById('users')) + '/:id'} element={<UserDetailPage />} />
          <Route path={childPath(routeById('devices'))} element={<Devices notice={notice} />} />
          <Route path={childPath(routeById('devices')) + '/:id'} element={<DeviceDetailPage />} />
          <Route path={childPath(routeById('sessions'))} element={<Sessions notice={notice} />} />
          <Route path={childPath(routeById('sessions')) + '/:id'} element={<SessionDetailPage />} />
          <Route path={childPath(routeById('audit'))} element={<Audit />} />
          <Route path={childPath(routeById('admins'))} element={<Admins notice={notice} />} />
          <Route path={childPath(routeById('risk'))} element={<Risk />} />
          <Route path={childPath(routeById('versions'))} element={<Versions />} />
          <Route
            path={childPath(routeById('config'))}
            element={configAllowed ? <ConfigCenter notice={notice} /> : <Forbidden />}
          />
          <Route path="*" element={<NotFound />} />
        </Route>
      </Routes>
    </AppProviders>
  );
}
