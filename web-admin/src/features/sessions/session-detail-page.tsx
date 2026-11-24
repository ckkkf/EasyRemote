// A8 会话详情：独立路由 /sessions/:id。
//
// 现状边界（不造数据）：
// - 会话字段来自 GET /admin/v1/sessions?q=<id>（服务端按会话 id 过滤，取精确匹配）；
// - 每秒质量曲线、链路切换记录、错误码时间线没有 telemetry 接口 → 明确「接口未提供」。
import { useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';

import {
  api,
  type LinkSwitch,
  type QualitySample,
  type SessionErrorRow,
  type SessionParty,
  type SessionRow,
  type TimelineEntry,
} from '../../api';
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
import { stateLabel, stateTone } from './session-states';

function fmtTime(iso: string | undefined): string {
  if (!iso || iso.startsWith('0001')) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString('zh-CN', { hour12: false });
}

function duration(s: SessionRow): string {
  const start = new Date(s.started_at).getTime();
  if (Number.isNaN(start)) return '—';
  const end = s.ended_at ? new Date(s.ended_at).getTime() : Date.now();
  const sec = Math.max(0, Math.floor((end - start) / 1000));
  if (sec < 60) return sec + ' 秒';
  const m = Math.floor(sec / 60);
  if (m < 60) return m + ' 分 ' + (sec % 60) + ' 秒';
  return Math.floor(m / 60) + ' 小时 ' + (m % 60) + ' 分';
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

export function SessionDetailPage() {
  const { id = '' } = useParams();
  const navigate = useNavigate();
  const [session, setSession] = useState<SessionRow | null>(null);
  const [quality, setQuality] = useState<{ available: boolean; samples: QualitySample[] } | null>(null);
  const [detail, setDetail] = useState<{
    controller: SessionParty;
    target: SessionParty;
    link_switches: LinkSwitch[];
    timeline: TimelineEntry[];
    error_events: SessionErrorRow[];
  } | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let alive = true;
    setLoading(true);
    (async () => {
      try {
        const res = await api.sessions(undefined, { q: id, limit: 5 });
        const found = res.items.find((s) => s.id === id) ?? null;
        if (!alive) return;
        setSession(found);
        setError(null);
        if (found) {
          const [q, d] = await Promise.all([api.sessionQuality(found.id), api.sessionDetail(found.id)]);
          if (!alive) return;
          setQuality({ available: q.available, samples: q.samples });
          setDetail(d);
        }
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

  if (loading) return <LoadingState text="读取会话…" />;
  if (error) return <ErrorState error={error} />;
  if (!session) {
    return (
      <Section title="会话不存在" subtitle={'会话 ID：' + (id || '—')}>
        <EmptyState text="该会话可能已被清理，或链接里的 ID 不正确。" />
        <div className="mt-4">
          <Button variant="ghost" size="sm" onClick={() => navigate('/sessions')}>
            返回会话列表
          </Button>
        </div>
      </Section>
    );
  }

  return (
    <div className="stack" data-page="session-detail">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 className="text-lg font-semibold">{session.id}</h3>
          <p className="text-sm text-muted-foreground">
            <Pill tone={stateTone(session.state)}>{stateLabel(session.state)}</Pill>
            <span className="ml-2">时长 {duration(session)}</span>
          </p>
        </div>
        <Button variant="ghost" size="sm" onClick={() => navigate('/sessions')}>
          返回列表
        </Button>
      </div>

      <Section title="会话信息" subtitle="来自 GET /admin/v1/sessions">
        <div className="kv">
          <span>会话 ID</span>
          <strong className="mono">{session.id}</strong>
          <span>状态</span>
          <strong>
            {stateLabel(session.state)}（{session.state}）
          </strong>
          <span>主控设备</span>
          <strong className="mono">{session.controller_id}</strong>
          <span>被控设备</span>
          <strong className="mono">{session.target_id}</strong>
          <span>开始时间</span>
          <strong>{fmtTime(session.started_at)}</strong>
          <span>结束时间</span>
          <strong>{fmtTime(session.ended_at)}</strong>
          <span>结束原因</span>
          <strong>{session.reason || '—'}</strong>
        </div>
      </Section>

      <Section
        title="质量采样（每秒）"
        subtitle={
          quality?.available
            ? '客户端上报的真实采样 · ' + quality.samples.length + ' 个点'
            : '该会话没有采样数据'
        }
      >
        {quality?.available ? (
          <div className="grid gap-3">
            <div className="flex flex-wrap gap-4 text-sm">
              <span>
                平均延迟{' '}
                <strong>{Math.round(quality.samples.reduce((n, s) => n + s.latency_ms, 0) / quality.samples.length)} ms</strong>
              </span>
              <span>
                平均丢包{' '}
                <strong>
                  {(quality.samples.reduce((n, s) => n + s.loss_permille, 0) / quality.samples.length / 10).toFixed(1)}%
                </strong>
              </span>
              <span>
                平均帧率{' '}
                <strong>{Math.round(quality.samples.reduce((n, s) => n + s.fps, 0) / quality.samples.length)} fps</strong>
              </span>
              <span>
                中继占比{' '}
                <strong>
                  {Math.round((quality.samples.filter((s) => s.relay).length / quality.samples.length) * 100)}%
                </strong>
              </span>
            </div>
            <DataTable
              ariaLabel="质量采样"
              rows={quality.samples.slice(-30)}
              rowKey={(s) => s.ts}
              columns={[
                { key: 'ts', header: '时间', className: 'muted', render: (s) => fmtTime(s.ts) },
                { key: 'latency', header: '延迟 (ms)', render: (s) => String(s.latency_ms) },
                { key: 'loss', header: '丢包 (‰)', render: (s) => String(s.loss_permille) },
                { key: 'fps', header: '帧率', render: (s) => String(s.fps) },
                { key: 'tier', header: '档位', render: (s) => s.tier || '—' },
                {
                  key: 'link',
                  header: '链路',
                  render: (s) => <Pill tone={s.relay ? 'warn' : 'ok'}>{s.relay ? '中继' : 'P2P'}</Pill>,
                },
              ]}
            />
          </div>
        ) : (
          <EmptyState
            text="该会话没有质量采样（客户端未上报或会话未建立数据面）。"
            hint="没有采样就不画曲线，避免用 0 值误导排障。"
          />
        )}
      </Section>

      <Section title="双方设备" subtitle="来自 GET /admin/v1/sessions/detail">
        {detail ? (
          <DataTable
            ariaLabel="会话双方设备"
            rows={[
              { role: '主控', party: detail.controller },
              { role: '被控', party: detail.target },
            ]}
            rowKey={(r) => r.role}
            columns={[
              { key: 'role', header: '角色', render: (r) => r.role },
              { key: 'code', header: '设备码', className: 'mono', render: (r) => r.party.device_code || '—' },
              { key: 'platform', header: '平台', render: (r) => r.party.platform || '—' },
              { key: 'os', header: '系统', className: 'muted', render: (r) => r.party.os_version || '—' },
              { key: 'client', header: '客户端', className: 'muted', render: (r) => r.party.client_version || '—' },
              {
                key: 'online',
                header: '在线',
                render: (r) => <Pill tone={r.party.online ? 'ok' : 'muted'}>{r.party.online ? '在线' : '离线'}</Pill>,
              },
            ]}
          />
        ) : (
          <EmptyState text="读取中…" />
        )}
      </Section>

      <Section title="链路切换" subtitle="来自客户端上报的 link 变化">
        {detail && detail.link_switches.length > 0 ? (
          <DataTable
            ariaLabel="链路切换"
            rows={detail.link_switches}
            rowKey={(s) => s.ts + s.from + s.to}
            columns={[
              { key: 'ts', header: '时间', className: 'muted', render: (s) => fmtTime(s.ts) },
              { key: 'from', header: '从', render: (s) => s.from },
              { key: 'to', header: '到', render: (s) => s.to },
            ]}
          />
        ) : (
          <EmptyState text="该会话没有链路切换记录（或客户端未上报链路类型）。" />
        )}
      </Section>

      <Section title="错误码时间线" subtitle="来自网关真实错误返回（ADM-API-03h）">
        {detail && detail.error_events.length > 0 ? (
          <DataTable
            ariaLabel="错误码时间线"
            rows={detail.error_events}
            rowKey={(e) => e.ts + e.name + e.source}
            columns={[
              { key: 'ts', header: '时间', className: 'muted', render: (e) => fmtTime(e.ts) },
              { key: 'code', header: '错误码', className: 'mono', render: (e) => String(e.code) + ' ' + e.name },
              { key: 'source', header: '来源', className: 'muted', render: (e) => e.source || '—' },
              { key: 'detail', header: '详情', className: 'muted', render: (e) => e.detail || '—' },
            ]}
          />
        ) : (
          <EmptyState text="该会话没有错误事件。" hint="只有网关按会话返回过错误才会有记录。" />
        )}
      </Section>

      <Section title="状态时间线" subtitle="会话创建与结束">
        {detail ? (
          <DataTable
            ariaLabel="状态时间线"
            rows={detail.timeline}
            rowKey={(t) => t.ts + t.label}
            columns={[
              { key: 'ts', header: '时间', className: 'muted', render: (t) => fmtTime(t.ts) },
              { key: 'label', header: '事件', render: (t) => t.label },
              { key: 'detail', header: '详情', className: 'muted', render: (t) => t.detail || '—' },
            ]}
          />
        ) : (
          <EmptyState text="读取中…" />
        )}
      </Section>
    </div>
  );
}
