import 'dart:async';

import 'package:flutter/material.dart';

import '../api.dart';
import '../models.dart';
import '../shell.dart';
import '../theme.dart';

/// 诊断页：把链路探测结果翻译成可执行建议（D1）。
/// 时延与丢包用真实 ping 网关的测量值；NAT/打洞需要桌面端 QUIC 栈，Web 预览下如实显示不可用。
class DiagnosticsPage extends StatefulWidget {
  const DiagnosticsPage({super.key, required this.client, this.config});

  final GatewayClient client;

  /// 服务端配置（P1-4）：`diag.auto_refresh_secs` 决定自动刷新间隔。
  final AppConfig? config;

  @override
  State<DiagnosticsPage> createState() => _DiagnosticsPageState();
}

class _DiagnosticsPageState extends State<DiagnosticsPage> {
  bool _running = false;
  Duration? _rtt;
  int _lossSamples = 0;
  int _lost = 0;
  String? _error;
  ReflectInfo? _reflect;
  bool _auto = true;
  Timer? _timer;

  int get _autoSecs => (widget.config?.diagAutoRefreshSecs ?? 30).clamp(5, 3600);

  @override
  void initState() {
    super.initState();
    _restartTimer();
  }

  @override
  void didUpdateWidget(covariant DiagnosticsPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    // 配置是异步拿到的：间隔变了就重排定时器。
    if (oldWidget.config?.diagAutoRefreshSecs != widget.config?.diagAutoRefreshSecs) {
      _restartTimer();
    }
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  /// 自动刷新：按服务端下发的间隔跑一次完整诊断；关掉开关或页面不可见时就停。
  void _restartTimer() {
    _timer?.cancel();
    if (!_auto) return;
    _timer = Timer.periodic(Duration(seconds: _autoSecs), (_) {
      if (!mounted || _running) return;
      _runDiagnostics();
    });
  }

  void _toggleAuto(bool on) {
    setState(() => _auto = on);
    _restartTimer();
  }

  Future<void> _runDiagnostics() async {
    if (_running) return;
    setState(() {
      _running = true;
      _error = null;
      _lost = 0;
      _lossSamples = 0;
      _rtt = null;
    });
    // 出口地址：服务端看到的 TCP 源地址，Web 端拿不到自己的公网地址就靠它。
    try {
      final info = await widget.client.netReflect();
      if (mounted) setState(() => _reflect = info);
    } catch (_) {
      // 诊断页不因为一个次要指标失败而整体报错。
    }
    final samples = <Duration>[];
    for (var i = 0; i < 5; i++) {
      final rtt = await widget.client.ping();
      if (!mounted) return;
      if (rtt == null) {
        _lost++;
      } else {
        samples.add(rtt);
      }
      _lossSamples++;
      setState(() {});
      await Future<void>.delayed(const Duration(milliseconds: 120));
    }
    if (!mounted) return;
    setState(() {
      _running = false;
      if (samples.isEmpty) {
        _error = '网关不可达，请检查服务是否在运行';
      } else {
        samples.sort((a, b) => a.compareTo(b));
        _rtt = samples[samples.length ~/ 2];
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    final wide = MediaQuery.sizeOf(context).width >= ErTokens.mobileBreakpoint;
    final loss = _lossSamples == 0 ? null : (_lost / _lossSamples * 100);

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        PageHeader(
          title: '网络诊断',
          subtitle: '连接异常时，这里会说明原因和可执行的处理方式 · 自动刷新 ${_autoSecs} 秒',
          actions: [
            Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Text('自动刷新', style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant)),
                Switch(value: _auto, onChanged: _toggleAuto),
              ],
            ),
            FilledButton.icon(
              onPressed: _running ? null : _runDiagnostics,
              icon: _running
                  ? const SizedBox(height: 16, width: 16, child: CircularProgressIndicator(strokeWidth: 2))
                  : const Icon(Icons.play_arrow_rounded, size: 18),
              label: Text(_running ? '诊断中…' : '开始诊断'),
            ),
          ],
        ),
        Expanded(
          child: ListView(
            padding: EdgeInsets.fromLTRB(wide ? 32 : 16, 0, wide ? 32 : 16, 32),
            children: [
              LayoutBuilder(
                builder: (context, constraints) {
                  final columns = constraints.maxWidth >= 900 ? 3 : constraints.maxWidth >= 560 ? 2 : 1;
                  final width = (constraints.maxWidth - (columns - 1) * 12) / columns;
                  return Wrap(
                    spacing: 12,
                    runSpacing: 12,
                    children: [
                      SizedBox(
                        width: width,
                        child: _MetricCard(
                          label: '网关往返时延',
                          value: _rtt == null ? '— ms' : _rtt!.inMilliseconds.toString() + ' ms',
                          hint: _rtt == null ? '点「开始诊断」实测' : '5 次采样取中位数',
                          tone: _rtt == null ? null : (_rtt!.inMilliseconds <= 60 ? _Tone.good : _Tone.warn),
                        ),
                      ),
                      SizedBox(
                        width: width,
                        child: _MetricCard(
                          label: '丢包率',
                          value: loss == null ? '— %' : loss.toStringAsFixed(0) + ' %',
                          hint: _lossSamples == 0 ? '尚未采样' : _lost.toString() + ' / ' + _lossSamples.toString() + ' 次失败',
                          tone: loss == null ? null : (loss <= 0 ? _Tone.good : _Tone.warn),
                        ),
                      ),
                      SizedBox(
                        width: width,
                        child: _MetricCard(
                          label: '出口地址（TCP）',
                          value: _reflect == null ? '—' : _reflect!.pretty,
                          hint: _reflect == null ? '点「开始诊断」获取' : '服务端看到的源地址，不是 NAT 探测结果',
                        ),
                      ),
                      SizedBox(
                        width: width,
                        child: _MetricCard(
                          label: '当前链路',
                          value: _rtt == null ? '未知' : '网关直连',
                          hint: 'P2P 打洞需要桌面端 QUIC 栈',
                        ),
                      ),
                      SizedBox(
                        width: width,
                        child: _MetricCard(
                          label: 'NAT 类型',
                          value: '待探测',
                          hint: 'Web 预览不支持 STUN 探测',
                        ),
                      ),
                      SizedBox(
                        width: width,
                        child: _MetricCard(
                          label: '打洞结果',
                          value: '未开始',
                          hint: '需要信令 + 中继环境',
                        ),
                      ),
                      SizedBox(
                        width: width,
                        child: _MetricCard(
                          label: '传输加密',
                          value: '开启',
                          hint: 'Ed25519 握手 · 中继只见密文',
                          tone: _Tone.good,
                        ),
                      ),
                    ],
                  );
                },
              ),
              const SizedBox(height: 20),
              Container(
                padding: const EdgeInsets.all(16),
                decoration: BoxDecoration(
                  color: scheme.surfaceContainerLow,
                  borderRadius: BorderRadius.circular(ErTokens.radiusLg),
                  border: Border.all(color: scheme.outlineVariant.withValues(alpha: 0.7)),
                ),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text('建议', style: text.titleSmall),
                    const SizedBox(height: 8),
                    _AdviceRow(
                      ok: _rtt != null,
                      text: _rtt == null
                          ? '先点右上角「开始诊断」，确认本机能连通网关。'
                          : '本机到网关 ' + _rtt!.inMilliseconds.toString() + ' ms，属于' +
                              (_rtt!.inMilliseconds <= 60 ? '良好' : '偏高') + '区间。',
                    ),
                    _AdviceRow(
                      ok: _lost == 0 && _lossSamples > 0,
                      text: _lossSamples == 0
                          ? '丢包率尚未采样，诊断完成后会给出结论。'
                          : (_lost == 0 ? '连续 5 次探测无丢包。' : '探测期间有 ' + _lost.toString() + ' 次失败，检查网络或服务负载。'),
                    ),
                    _AdviceRow(
                      ok: _reflect != null,
                      text: _reflect == null
                          ? '出口地址尚未取到：诊断会一并请求 /v1/net/reflect。'
                          : '服务端看到的出口地址是 ${_reflect!.pretty}（TCP 源地址）。',
                    ),
                    const _AdviceRow(
                      ok: false,
                      text: 'P2P 直连、NAT 类型与打洞结果需要桌面端（QUIC/STUN）才能给出真实值。',
                    ),
                    if (_error != null) ...[
                      const SizedBox(height: 6),
                      Text(_error!, style: text.bodySmall?.copyWith(color: scheme.error)),
                    ],
                  ],
                ),
              ),
              const SizedBox(height: 12),
              Row(
                children: [
                  Icon(Icons.privacy_tip_outlined, size: 15, color: scheme.onSurfaceVariant),
                  const SizedBox(width: 6),
                  Expanded(
                    child: Text('诊断数据仅用于本机展示，不会上传到服务端。',
                        style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant)),
                  ),
                ],
              ),
            ],
          ),
        ),
      ],
    );
  }
}

enum _Tone { good, warn }

class _MetricCard extends StatelessWidget {
  const _MetricCard({required this.label, required this.value, required this.hint, this.tone});

  final String label;
  final String value;
  final String hint;
  final _Tone? tone;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    final color = switch (tone) {
      _Tone.good => const Color(0xFF2A6239),
      _Tone.warn => const Color(0xFF78510B),
      null => scheme.onSurface,
    };
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: scheme.surfaceContainerLow,
        borderRadius: BorderRadius.circular(ErTokens.radiusLg),
        border: Border.all(color: scheme.outlineVariant.withValues(alpha: 0.7)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(label, style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant)),
          const SizedBox(height: 6),
          Text(value, style: text.titleLarge?.copyWith(color: color)),
          const SizedBox(height: 2),
          Text(hint, style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant.withValues(alpha: 0.85))),
        ],
      ),
    );
  }
}

class _AdviceRow extends StatelessWidget {
  const _AdviceRow({required this.ok, required this.text});

  final bool ok;
  final String text;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final textTheme = Theme.of(context).textTheme;
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 4),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(ok ? Icons.check_circle_rounded : Icons.info_outline_rounded,
              size: 16, color: ok ? const Color(0xFF2A6239) : scheme.onSurfaceVariant),
          const SizedBox(width: 8),
          Expanded(child: Text(text, style: textTheme.bodySmall)),
        ],
      ),
    );
  }
}
