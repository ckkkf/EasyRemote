// A4 用户详情：独立路由 /users/:id（可深链、可刷新）。
//
// 现状边界（不造数据）：
// - 账号信息来自 GET /admin/v1/users?q=<id>（服务端按 id 搜索，取精确匹配）；
// - 绑定设备来自 GET /admin/v1/devices?q=<account_id>（服务端按账号过滤，再本地精确校验）；
// - 最近会话 / 操作记录 / 备注没有按账号查询的接口 → 明确显示「接口未开放」，不做前端假过滤。
import { useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';

import { api, type AuditRow, type DeviceRow, type SessionRow, type UserRow } from '../../api';
import { useCan } from '../../app/providers';
import { DataTable } from '../../components/business/data-table';
import { EmptyState, ErrorState, LoadingState } from '../../components/business/page-state';
import { Pill } from '../../components/business/pill';
import { Button } from '../../components/ui/button';
import {
  Card as UiCard,
  CardContent as UiCardContent,
  CardDescription as UiCardDescription,
  CardHeader as UiCardHeader,
  CardTitle as UiCardTitle,
} from '../../components/ui/card';

function fmtTime(iso: string): string {
  if (!iso || iso.startsWith('0001')) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString('zh-CN', { hour12: false });
}

/** 设备码展示统一每 3 位加空格（与列表页一致）。 */
function fmtCode(code: string): string {
  const clean = (code || '').replace(/\s/g, '');
  if (clean.length !== 9) return code;
  return clean.slice(0, 3) + ' ' + clean.slice(3, 6) + ' ' + clean.slice(6);
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

function Section({ title, subtitle, children }: { title: string; subtitle?: string; children: React.ReactNode }) {
  return (
    <UiCard>
      <UiCardHeader>
        <UiCardTitle>{title}</UiCardTitle>
        {subtitle ? <UiCardDescription>{subtitle}</UiCardDescription> : null}
      </UiCardHeader>
      <UiCardContent>{children}</UiCardContent>
    </UiCard>
  );
}

export function UserDetailPage() {
  const { id = '' } = useParams();
  const navigate = useNavigate();
  const [user, setUser] = useState<UserRow | null>(null);
  const [devices, setDevices] = useState<DeviceRow[] | null>(null);
  const [sessions, setSessions] = useState<SessionRow[]>([]);
  const [audits, setAudits] = useState<AuditRow[]>([]);
  const [note, setNote] = useState('');
  const [noteMeta, setNoteMeta] = useState<{ by: string; at: string }>({ by: '', at: '' });
  const [noteSaving, setNoteSaving] = useState(false);
  const [noteSaved, setNoteSaved] = useState('');
  const canWriteNote = useCan('user.write');
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let alive = true;
    setLoading(true);
    (async () => {
      try {
        const list = await api.users({ q: id, limit: 5 });
        const found = list.items.find((u) => u.id === id) ?? null;
        if (!alive) return;
        setUser(found);
        setError(null);
        if (!found) {
          setDevices([]);
          return;
        }
        const [dev, ses, aud, n] = await Promise.all([
          api.devices({ q: id, limit: 50 }),
          api.userSessions(id, 20),
          api.audit({ object: id, limit: 20 }),
          api.userNote(id),
        ]);
        if (!alive) return;
        setDevices(dev.items.filter((d) => d.account_id === id));
        setSessions(ses.items);
        setAudits(aud.items);
        setNote(n.note);
        setNoteMeta({ by: n.updated_by, at: n.updated_at ?? '' });
      } catch (err) {
        if (alive) setError(err);
      } finally {
        if (alive) setLoading(false);
      }
    })();
    return () => {
      alive = false;
    };
  }, [id]);

  if (loading) return <LoadingState text="读取用户…" />;
  if (error) return <ErrorState error={error} />;
  if (!user) {
    return (
      <Section title="用户不存在" subtitle={'账号 ID：' + (id || '—')}>
        <EmptyState text="该账号可能已删除，或链接里的 ID 不正确。" />
        <div className="mt-4">
          <Button variant="ghost" size="sm" onClick={() => navigate('/users')}>
            返回用户列表
          </Button>
        </div>
      </Section>
    );
  }

  return (
    <div className="stack" data-page="user-detail">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 className="text-lg font-semibold">{user.id}</h3>
          <p className="text-sm text-muted-foreground">
            手机号 {user.phone_mask}（默认脱敏，查看明文需二次授权并审计）
          </p>
        </div>
        <Button variant="ghost" size="sm" onClick={() => navigate('/users')}>
          返回列表
        </Button>
      </div>

      <Section title="账号信息" subtitle="来自 GET /admin/v1/users">
        <div className="kv">
          <span>账号 ID</span>
          <strong className="mono">{user.id}</strong>
          <span>昵称</span>
          <strong>{user.nickname || '—'}</strong>
          <span>手机号</span>
          <strong>{user.phone_mask}</strong>
          <span>状态</span>
          <strong>
            <Pill tone={user.status === 'active' ? 'ok' : 'danger'}>{user.status}</Pill>
          </strong>
          <span>设备数</span>
          <strong>{user.device_count}</strong>
          <span>注册时间</span>
          <strong>{fmtTime(user.created_at)}</strong>
          <span>最近登录</span>
          <strong>{relTime(user.last_login_at ?? '')}</strong>
        </div>
        <p className="mt-3 text-xs text-muted-foreground">
          昵称/最近登录/设备数已由服务端提供（ADM-API-03a/03b）；历史会话数与封禁记录仍无接口（ADM-API-03）。昵称为空表示运营未设置。
        </p>
      </Section>

      <Section title="绑定设备" subtitle={'服务端按账号过滤 · ' + (devices?.length ?? 0) + ' 台'}>
        {devices && devices.length > 0 ? (
          <DataTable
            ariaLabel="绑定设备"
            rows={devices}
            rowKey={(d) => d.id || d.device_code}
            columns={[
              { key: 'code', header: '设备码', className: 'mono big', render: (d) => fmtCode(d.device_code) },
              { key: 'platform', header: '平台', render: (d) => d.platform },
              {
                key: 'online',
                header: '在线',
                render: (d) => <Pill tone={d.online ? 'ok' : 'muted'}>{d.online ? '在线' : '离线'}</Pill>,
              },
              { key: 'seen', header: '最后在线', className: 'muted', render: (d) => relTime(d.last_seen || '') },
            ]}
          />
        ) : (
          <EmptyState text="这个账号还没有绑定设备。" />
        )}
      </Section>

      <Section title="最近会话" subtitle={'服务端按账号过滤 · ' + sessions.length + ' 条'}>
        {sessions.length === 0 ? (
          <EmptyState text="这个账号还没有会话记录。" />
        ) : (
          <DataTable
            ariaLabel="账号最近会话"
            rows={sessions}
            rowKey={(s) => s.id}
            onRowClick={(s) => navigate('/sessions/' + s.id)}
            columns={[
              { key: 'id', header: '会话 ID', className: 'mono', render: (s) => s.id },
              { key: 'state', header: '状态', render: (s) => <Pill tone={s.state === 'closed' ? 'muted' : 'warn'}>{s.state}</Pill> },
              { key: 'started', header: '开始时间', className: 'muted', render: (s) => fmtTime(s.started_at) },
              { key: 'reason', header: '结束原因', className: 'muted', render: (s) => s.reason || '—' },
            ]}
          />
        )}
      </Section>

      <Section title="操作记录" subtitle={'来自审计 · object=' + id}>
        {audits.length === 0 ? (
          <EmptyState text="这个账号还没有被操作过。" />
        ) : (
          <DataTable
            ariaLabel="账号操作记录"
            rows={audits}
            rowKey={(a) => a.id}
            columns={[
              { key: 'ts', header: '时间', className: 'muted', render: (a) => fmtTime(a.ts) },
              { key: 'actor', header: '操作人', render: (a) => a.actor },
              { key: 'action', header: '动作', className: 'mono', render: (a) => a.action },
              { key: 'result', header: '结果', render: (a) => a.result },
            ]}
          />
        )}
      </Section>

      <Section
        title="备注"
        subtitle={
          noteMeta.by
            ? '最后更新：' + noteMeta.by + (noteMeta.at ? ' · ' + fmtTime(noteMeta.at) : '')
            : '还没有备注'
        }
      >
        <div className="grid gap-2">
          <textarea
            aria-label="账号备注"
            rows={3}
            value={note}
            disabled={!canWriteNote || noteSaving}
            onChange={(e) => {
              setNote(e.target.value);
              setNoteSaved('');
            }}
          />
          <div className="flex items-center gap-3">
            <Button
              size="sm"
              disabled={!canWriteNote || noteSaving}
              onClick={() => {
                setNoteSaving(true);
                setError(null);
                api
                  .setUserNote(id, note)
                  .then(() => {
                    setNoteSaved('已保存（写入审计）');
                  })
                  .catch((err) => setError(err))
                  .finally(() => setNoteSaving(false));
              }}
            >
              保存备注
            </Button>
            {!canWriteNote ? <span className="text-xs text-muted-foreground">当前角色无备注写权限</span> : null}
            {noteSaved ? <span className="text-xs text-muted-foreground">{noteSaved}</span> : null}
          </div>
        </div>
      </Section>
    </div>
  );
}
