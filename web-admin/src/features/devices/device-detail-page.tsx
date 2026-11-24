// A6 设备详情：独立路由 /devices/:id（客服排查主入口）。
//
// 现状边界（不造数据）：
// - 设备信息来自 GET /admin/v1/devices?q=<id>（服务端搜索，取精确匹配）；
// - 最近会话来自 GET /admin/v1/sessions?q=<device_id>（服务端按主控/被控过滤，真实分页）；
// - 近 30 天质量曲线没有 telemetry 接口 → 明确「接口未提供」，不画假曲线；
// - 强制下线/解绑/标记风险没有接口 → 按钮禁用并注明原因。
import { useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';

import { api, type DeviceQualityDay, type DeviceRow, type SessionRow } from '../../api';
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

function fmtCode(code: string): string {
  const clean = (code || '').replace(/\s/g, '');
  if (clean.length !== 9) return code;
  return clean.slice(0, 3) + ' ' + clean.slice(3, 6) + ' ' + clean.slice(6);
}

function fmtTime(iso: string): string {
  if (!iso || iso.startsWith('0001')) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString('zh-CN', { hour12: false });
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

export function DeviceDetailPage() {
  const { id = '' } = useParams();
  const navigate = useNavigate();
  const [device, setDevice] = useState<DeviceRow | null>(null);
  const [sessions, setSessions] = useState<SessionRow[] | null>(null);
  const [quality, setQuality] = useState<{ available: boolean; days: DeviceQualityDay[] } | null>(null);
  const [range, setRange] = useState(30);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let alive = true;
    setLoading(true);
    (async () => {
      try {
        const list = await api.devices({ q: id, limit: 5 });
        const found =
          list.items.find((d) => d.id === id) ??
          list.items.find((d) => d.device_code.replace(/\s/g, '') === id) ??
          null;
        if (!alive) return;
        setDevice(found);
        setError(null);
        if (!found) {
          setSessions([]);
          return;
        }
        // 服务端按主控/被控设备过滤（store.ListSessions 的 q 覆盖 controller/target id）。
        const [res, q] = await Promise.all([
          api.sessions(undefined, { q: found.id, limit: 20 }),
          api.deviceQuality(found.id, 30),
        ]);
        if (!alive) return;
        setSessions(res.items);
        setQuality({ available: q.available, days: q.days });
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

  if (loading) return <LoadingState text="读取设备…" />;
  if (error) return <ErrorState error={error} />;
  if (!device) {
    return (
      <Section title="设备不存在" subtitle={'设备标识：' + (id || '—')}>
        <EmptyState text="该设备可能已解绑，或链接里的标识不正确。" />
        <div className="mt-4">
          <Button variant="ghost" size="sm" onClick={() => navigate('/devices')}>
            返回设备列表
          </Button>
        </div>
      </Section>
    );
  }

  return (
    <div className="stack" data-page="device-detail">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 className="text-lg font-semibold">{fmtCode(device.device_code)}</h3>
          <p className="text-sm text-muted-foreground">
            {device.platform} · 归属 {device.account_id}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => void navigator.clipboard?.writeText(device.device_code.replace(/\s/g, ''))}
          >
            复制设备码
          </Button>
          {/* 强制下线在列表页按行操作（需要选择设备 + 填原因）；详情页不放重复入口。 */}
          <Button variant="ghost" size="sm" onClick={() => navigate('/devices')}>
            返回列表
          </Button>
        </div>
      </div>

      <Section title="设备信息" subtitle="来自 GET /admin/v1/devices">
        <div className="kv">
          <span>设备码</span>
          <strong className="mono big">{fmtCode(device.device_code)}</strong>
          <span>设备 ID</span>
          <strong className="mono">{device.id}</strong>
          <span>归属账号</span>
          <strong className="mono">{device.account_id}</strong>
          <span>设备名</span>
          <strong>{device.device_name || '—'}</strong>
          <span>平台</span>
          <strong>{device.platform}</strong>
          <span>系统版本</span>
          <strong>{device.os_version || '—'}</strong>
          <span>客户端版本</span>
          <strong>{device.client_version || '—'}</strong>
          <span>NAT 类型</span>
          <strong>{device.nat_type || '—'}</strong>
          <span>在线</span>
          <strong>
            <Pill tone={device.online ? 'ok' : 'muted'}>{device.online ? '在线' : '离线'}</Pill>
          </strong>
          <span>最后在线</span>
          <strong>{relTime(device.last_seen || device.last_seen_at || '')}</strong>
        </div>
        <p className="mt-3 text-xs text-muted-foreground">
          设备名/系统版本/客户端版本/NAT 由客户端绑定上报（ADM-API-04a）；显示「—」表示该设备尚未上报。
        </p>
      </Section>

      <Section
        title="近 30 天质量"
        subtitle={quality?.available ? '按日聚合 · ' + quality.days.length + ' 天有采样' : '该设备没有质量采样'}
      >
        {quality?.available ? (
          <DataTable
            ariaLabel="设备质量按日聚合"
            rows={quality.days.slice(-range)}
            rowKey={(d) => d.date}
            columns={[
              { key: 'date', header: '日期', className: 'mono', render: (d) => d.date },
              { key: 'latency', header: '平均延迟 (ms)', render: (d) => String(d.avg_latency_ms) },
              { key: 'loss', header: '平均丢包 (‰)', render: (d) => String(d.avg_loss_permille) },
              { key: 'relay', header: '中继占比', render: (d) => d.relay_percent + '%' },
              { key: 'samples', header: '采样数', className: 'muted', render: (d) => String(d.samples) },
            ]}
          />
        ) : (
          <EmptyState
            text="该设备还没有质量采样（客户端未上报或会话未建立数据面）。"
            hint="没有采样就不画曲线，避免用 0 值误导排障。"
          />
        )}
        <div className="mt-3 flex gap-2">
          <Button variant="outline" size="sm" onClick={() => setRange(7)}>
            近 7 天
          </Button>
          <Button variant="outline" size="sm" onClick={() => setRange(30)}>
            近 30 天
          </Button>
        </div>
      </Section>

      <Section title="最近会话" subtitle={'服务端按设备过滤 · ' + (sessions?.length ?? 0) + ' 条'}>
        {sessions && sessions.length > 0 ? (
          <DataTable
            ariaLabel="设备最近会话"
            rows={sessions}
            rowKey={(s) => s.id}
            columns={[
              { key: 'id', header: '会话 ID', className: 'mono', render: (s) => s.id },
              {
                key: 'state',
                header: '状态',
                render: (s) => (
                  <Pill tone={s.state === 'active' ? 'ok' : s.state === 'closed' ? 'muted' : 'warn'}>{s.state}</Pill>
                ),
              },
              { key: 'duration', header: '时长', render: (s) => duration(s) },
              { key: 'started', header: '开始时间', className: 'muted', render: (s) => fmtTime(s.started_at) },
            ]}
          />
        ) : (
          <EmptyState text="这台设备还没有会话记录。" />
        )}
        <p className="mt-3 text-xs text-muted-foreground">
          会话详情（每秒质量曲线、链路切换、错误码时间线）需要 A8 页面与 telemetry 接口（ADM-UI-07 / ADM-API-02）。
        </p>
      </Section>
    </div>
  );
}
