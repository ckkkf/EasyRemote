// ADM-UI-04：仪表盘只允许展示权威字段，缺接口的指标显示"数据未提供"，刷新语义符合 PRD 5s。
import { describe, expect, it } from 'vitest';
import {
  POLL_INTERVAL_MS,
  STALE_AFTER_MS,
  UNAVAILABLE_HINT,
  buildMetricGroups,
  formatUpdatedAt,
  isStale,
  onlineRate,
  trendPath,
} from '../src/features/dashboard/metrics';
import type { Overview } from '../src/api';

const overview: Overview = {
  devices_total: 10,
  devices_online: 4,
  users_online: 3,
  sessions_active: 2,
  sessions_pending: 1,
  quality_available: true,
  latency_p50_ms: 28,
  latency_p95_ms: 64,
  relay_percent: 32,
  quality_samples: 120,
  stability_available: true,
  closed_sessions: 8,
  abnormal_closes: 2,
  abnormal_close_rate: 25,
  connect_available: true,
  connect_attempts: 20,
  connect_success_rate: 90,
  link_available: true,
  p2p_hit_rate: 75,
  fps_available: true,
  fps_target_rate: 88,
  devices_new_available: true,
  devices_new_24h: 6,
  crash_available: true,
  crash_rate: 4,
  reconnect_available: true,
  reconnect_success_rate: 92,
  generated_at: '2026-11-23T03:00:00Z',
};

describe('dashboard metrics', () => {
  it('TestMetricGroupsOnlyUseAuthoritativeFields', () => {
    const groups = buildMetricGroups(overview);
    expect(groups.map((g) => g.id)).toEqual(['scale', 'quality', 'stability']);

    const scale = groups[0].items;
    const byKey = Object.fromEntries(scale.map((i) => [i.key, i.value]));
    expect(byKey.devices_online).toBe('4');
    expect(byKey.devices_total).toBe('10');
    expect(byKey.sessions_active).toBe('2');
    expect(byKey.sessions_pending).toBe('1');
    // 契约里没有的字段必须是 null，不能猜、不能算。
    // users_online 由 ADM-API-02a 提供；24h 新增设备由 ADM-API-02f6（设备创建时间）提供。
    expect(byKey.users_online).toBe('3');
    expect(byKey.devices_new_24h).toBe('6');

    // 质量组：有采样时 P50/P95/中继占比是真值，其余仍为「数据未提供」。
    const quality = Object.fromEntries(groups[1].items.map((i) => [i.key, i.value]));
    expect(quality.relay_ratio).toBe('32%');
    expect(quality.latency_p50).toBe('28 ms');
    expect(quality.latency_p95).toBe('64 ms');
    expect(quality.connect_success_rate).toBe('90%');
    expect(quality.p2p_hit_rate).toBe('75%');
    expect(quality.fps_target_rate).toBe('88%');
    // 稳定性组：异常中断率来自结束原因归一统计；崩溃率/重连成功率仍无数据源。
    const stability = Object.fromEntries(groups[2].items.map((i) => [i.key, i.value]));
    expect(stability.abnormal_close_rate).toBe('25%');
    expect(stability.client_crash_rate).toBe('4%');
    expect(stability.reconnect_success_rate).toBe('92%');
    expect(groups[1].items).toHaveLength(6);
    expect(groups[2].items).toHaveLength(3);
  });

  it('TestTrendPathMapsBuckets', () => {
    // ADM-API-02f3：趋势折线来自真实时间桶；空序列返回空串，峰值应落在顶部。
    expect(trendPath([], 'sessions_started')).toBe('');
    const points = [
      { bucket_start: '2026-11-23T10:00:00Z', sessions_started: 0, connect_attempts: 1, connect_active: 0 },
      { bucket_start: '2026-11-23T11:00:00Z', sessions_started: 2, connect_attempts: 3, connect_active: 1 },
      { bucket_start: '2026-11-23T12:00:00Z', sessions_started: 1, connect_attempts: 0, connect_active: 0 },
    ];
    const path = trendPath(points, 'sessions_started', 320, 64);
    const coords = path.split(' ');
    expect(coords).toHaveLength(3);
    const ys = coords.map((c) => Number(c.split(',')[1]));
    // 峰值（第 2 个点 = 2）y 最小；0 值点在最底部附近。
    expect(ys[1]).toBeLessThan(ys[0]);
    expect(ys[1]).toBeLessThan(ys[2]);
    expect(ys[0]).toBeGreaterThan(50);
  });

  it('TestStabilityWithoutClosedSessionsIsUnavailable', () => {
    // 窗口内没有结束会话时，0/0 不能被画成 0%。
    const groups = buildMetricGroups({ ...overview, stability_available: false, closed_sessions: 0 });
    const stability = Object.fromEntries(groups[2].items.map((i) => [i.key, i.value]));
    expect(stability.abnormal_close_rate).toBeNull();
  });

  it('TestQualityWithoutSamplesIsUnavailable', () => {
    // 没有采样（quality_available=false）时不能把 0 当数据显示。
    const groups = buildMetricGroups({ ...overview, quality_available: false, quality_samples: 0 });
    const quality = Object.fromEntries(groups[1].items.map((i) => [i.key, i.value]));
    expect(quality.relay_ratio).toBeNull();
    expect(quality.latency_p50).toBeNull();
    expect(quality.latency_p95).toBeNull();
  });

  it('TestUnavailableMetricsAreNotZero', () => {
    const all = buildMetricGroups(null).flatMap((g) => g.items);
    for (const item of all) {
      expect(item.value).not.toBe('0');
      if (item.value === null) expect(item.hint).toBe('数据未提供');
    }
    // 没有 overview 时，连规模组也不能编数字。
    expect(all.filter((i) => i.value !== null)).toHaveLength(0);
  });

  it('TestPollIntervalMatchesPrd', () => {
    expect(POLL_INTERVAL_MS).toBe(5000);
    expect(STALE_AFTER_MS).toBe(15000);
  });

  it('TestStaleThreshold', () => {
    const now = 1_000_000;
    expect(isStale(null, now)).toBe(true);
    expect(isStale(now - 14_999, now)).toBe(false);
    expect(isStale(now - 15_000, now)).toBe(true);
    expect(formatUpdatedAt(null)).toBe('—');
  });

  it('TestOnlineRateUsesAuthoritativeTotal', () => {
    // 在线率必须用 overview 的总数，不能用列表接口分页后的一页样本。
    expect(onlineRate(overview)).toBe(40);
    expect(onlineRate({ ...overview, devices_total: 0, devices_online: 0 })).toBeNull();
    expect(onlineRate(null)).toBeNull();
  });
});
