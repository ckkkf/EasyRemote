// 仪表盘指标口径：只允许展示后端权威字段，缺接口的一律标「数据未提供」。
//
// PRD §6.3.1 要求规模/质量/稳定性三组指标与 24h/7d/30d 趋势，但当前契约只有
// Overview{devices_total, devices_online, sessions_active, sessions_pending, generated_at}。
// 质量/稳定性/趋势所需 telemetry 尚未提供（ADM-API-02），所以这里把它们建成
// available=false 的条目，由界面显示「数据未提供」——绝不用 0、样例百分比或假曲线填充。
import type { Overview, TrendPoint } from '../../api';

/** PRD §6.3.1：实时看板 5s 刷新。 */
export const POLL_INTERVAL_MS = 5_000;
/** 超过 3 个刷新周期没有成功更新，就标记为陈旧。 */
export const STALE_AFTER_MS = 15_000;

export const UNAVAILABLE_HINT = '数据未提供';

export type MetricItem = {
  key: string;
  label: string;
  /** null 表示后端没有该字段；界面显示「数据未提供」。 */
  value: string | null;
  hint?: string;
  /** 点击后跳转的页面（仅规模组有）。 */
  page?: 'devices' | 'sessions' | 'users';
};

export type MetricGroup = {
  id: 'scale' | 'quality' | 'stability';
  title: string;
  subtitle: string;
  items: MetricItem[];
};

function count(v: number | undefined): string | null {
  return typeof v === 'number' ? String(v) : null;
}

/** 规模组：有值就显示值，没有值同样标「数据未提供」，不留空单元格。 */
function metric(key: string, label: string, value: string | null, page?: MetricItem['page']): MetricItem {
  return { key, label, value, page, hint: value === null ? UNAVAILABLE_HINT : undefined };
}

export function buildMetricGroups(overview: Overview | null): MetricGroup[] {
  return [
    {
      id: 'scale',
      title: '规模',
      subtitle: '来自 GET /admin/v1/stats/overview',
      items: [
        metric('devices_online', '在线设备', count(overview?.devices_online), 'devices'),
        metric('devices_total', '设备总数', count(overview?.devices_total), 'devices'),
        metric('sessions_active', '进行中会话', count(overview?.sessions_active), 'sessions'),
        metric('sessions_pending', '待授权会话', count(overview?.sessions_pending), 'sessions'),
        metric('users_online', '在线用户', count(overview?.users_online), 'users'),
        metric(
          'devices_new_24h',
          '24h 新增设备',
          overview?.devices_new_available ? count(overview.devices_new_24h) : null,
          'devices',
        ),
      ],
    },
    {
      id: 'quality',
      title: '质量',
      subtitle: overview?.quality_available
        ? '来自客户端采样 · ' + (overview?.quality_samples ?? 0) + ' 个点（ADM-API-02e）'
        : '窗口内没有采样',
      // 顺序与设计稿一致：建连 / P2P / 中继 / P50 / P95 / 帧率；有采样时用真实分位数，没有采样绝不填 0。
      items: [
        {
          key: 'connect_success_rate',
          label: '建连成功率',
          // 只有窗口内确有尝试时才显示；0/0 不画成 0%。
          value: overview?.connect_available ? String(overview.connect_success_rate) + '%' : null,
          hint: overview?.connect_available
            ? '窗口内尝试 ' + (overview?.connect_attempts ?? 0) + ' 次'
            : UNAVAILABLE_HINT,
        },
        {
          key: 'p2p_hit_rate',
          label: 'P2P 命中率',
          // 只有客户端上报过 link 才统计；服务端不根据中继票据反推。
          value: overview?.link_available ? String(overview.p2p_hit_rate) + '%' : null,
          hint: overview?.link_available ? '来自客户端上报的链路类型' : UNAVAILABLE_HINT,
        },
        {
          key: 'relay_ratio',
          label: '中继占比',
          value: overview?.quality_available ? String(overview.relay_percent) + '%' : null,
          hint: overview?.quality_available ? undefined : UNAVAILABLE_HINT,
        },
        {
          key: 'latency_p50',
          label: '延迟 P50',
          value: overview?.quality_available ? String(overview.latency_p50_ms) + ' ms' : null,
          hint: overview?.quality_available ? undefined : UNAVAILABLE_HINT,
        },
        {
          key: 'latency_p95',
          label: '延迟 P95',
          value: overview?.quality_available ? String(overview.latency_p95_ms) + ' ms' : null,
          hint: overview?.quality_available ? undefined : UNAVAILABLE_HINT,
        },
        {
          key: 'fps_target_rate',
          label: '帧率达成率',
          value: overview?.fps_available ? String(overview.fps_target_rate) + '%' : null,
          hint: overview?.fps_available ? '实际帧率 ≥ 目标帧率的采样占比' : UNAVAILABLE_HINT,
        },
      ],
    },
    {
      id: 'stability',
      title: '稳定性',
      subtitle: overview?.stability_available
        ? '窗口内结束会话 ' + (overview?.closed_sessions ?? 0) + ' 次（ADM-API-02f1）'
        : '窗口内没有结束的会话',
      items: [
        {
          key: 'abnormal_close_rate',
          label: '会话异常中断率',
          // 只有窗口内确有结束会话时才显示比率，避免 0/0 被画成 0%。
          value: overview?.stability_available ? String(overview.abnormal_close_rate) + '%' : null,
          hint: overview?.stability_available ? undefined : UNAVAILABLE_HINT,
        },
        {
          key: 'client_crash_rate',
          label: '客户端崩溃率',
          value: overview?.crash_available ? String(overview.crash_rate) + '%' : null,
          hint: overview?.crash_available ? '崩溃事件 / 窗口内会话数' : UNAVAILABLE_HINT,
        },
        {
          key: 'reconnect_success_rate',
          label: '重连成功率',
          value: overview?.reconnect_available ? String(overview.reconnect_success_rate) + '%' : null,
          hint: overview?.reconnect_available ? '成功重连 / 重连尝试' : UNAVAILABLE_HINT,
        },
      ],
    },
  ];
}

/**
 * 把时间桶序列映射成 SVG polyline 的 points 字符串（ADM-API-02f3）。
 * 纯函数：不依赖 DOM，便于单测；空序列返回空串（调用方据此显示「暂无数据」）。
 */
export function trendPath(
  points: readonly TrendPoint[],
  key: 'sessions_started' | 'connect_attempts' | 'connect_active',
  width = 320,
  height = 64,
): string {
  if (points.length === 0) return '';
  const values = points.map((p) => p[key]);
  const max = Math.max(1, ...values);
  const step = points.length > 1 ? width / (points.length - 1) : 0;
  return values
    .map((v, i) => {
      const x = points.length > 1 ? i * step : width / 2;
      const y = height - (v / max) * (height - 4) - 2;
      return x.toFixed(1) + ',' + y.toFixed(1);
    })
    .join(' ');
}

/** 更新时间展示；从未成功更新过则显示破折号。 */
export function formatUpdatedAt(ms: number | null): string {
  if (ms === null) return '—';
  return new Date(ms).toLocaleTimeString('zh-CN', { hour12: false });
}

/** 是否陈旧：从未更新或超过 STALE_AFTER_MS 未更新。 */
export function isStale(lastUpdatedAt: number | null, now: number): boolean {
  if (lastUpdatedAt === null) return true;
  return now - lastUpdatedAt >= STALE_AFTER_MS;
}

/** 在线率只允许用权威计数（total 来自 overview），不能用分页后的一页样本。 */
export function onlineRate(overview: Overview | null): number | null {
  if (!overview || typeof overview.devices_total !== 'number' || overview.devices_total <= 0) return null;
  return Math.round((overview.devices_online / overview.devices_total) * 100);
}
