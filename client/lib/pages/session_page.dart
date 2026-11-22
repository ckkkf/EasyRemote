import 'dart:async';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../api.dart';
import '../models.dart';
import '../net_probe.dart';

import '../net_probe_cache.dart';
import '../session_video.dart';
import '../theme.dart';
import 'diagnostics_page.dart';

/// 会话页：请求中 → 等待授权 → 建立连接 → 会话中。
///
/// 版式按原型 1:1 对齐：
/// - s04 等待授权：设备信息头 + 一行小字三步进度 + 右侧「连接信息」卡 + 取消连接；
/// - s05 会话中：全屏远端画面占位 + 悬浮工具条（质量详情 / 全屏 / 断开连接）；
/// - s06 质量详情：右侧抽屉面板，RTT/丢包取真实 ping 与 /v1/net/reflect，码率/帧率标「桌面端」。
///
/// 状态说明（与 server 的真实能力一致）：HTTP 网关只会把会话推进到 authorized
/// （对方已授权，/v1/relay/ticket 可签发），active 由桌面端 Rust 核心在解出第一帧后回写。
/// 因此本页把 authorized 与 active 都算「已建立」，并用状态徽标区分是
/// 「已授权 · 通道建立中」还是「会话进行中」，不假装已经拿到画面。
class SessionPage extends StatefulWidget {
  const SessionPage({
    super.key,
    required this.client,
    required this.sessionId,
    required this.onLeave,
    this.targetCode = '',
    this.ownDevice,
    this.assist = false,
  });

  final GatewayClient client;
  final String sessionId;
  final VoidCallback onLeave;

  /// 目标设备码（9 位，来自连接页输入；协助码接入时为空）。
  final String targetCode;

  /// 本机设备（用于「连接信息 · 本机设备码」）。
  final DeviceInfo? ownDevice;

  /// true 表示本次是协助码接入（连接方式一栏要如实写）。
  final bool assist;

  @override
  State<SessionPage> createState() => _SessionPageState();
}

class _SessionPageState extends State<SessionPage> {
  SessionStatus _status = const SessionStatus(SessionState.requesting);
  Timer? _poll;
  Timer? _tick;
  bool _polling = false;
  final DateTime _openedAt = DateTime.now();
  DateTime? _liveSince;

  /// 目标设备信息：只有目标设备属于本账号时才能从 /v1/devices 查到平台与在线状态，
  /// 查到就显示真值，查不到就写「未知（需桌面端）」，不猜。
  DeviceInfo? _target;

  /// 悬浮质量浮层用的轻量链路采样（5 次 ping）。
  Duration? _rtt;
  int _samples = 0;
  int _lost = 0;

  /// 本机 NAT 实测结果（与诊断页同一套 STUN 探测）。
  NatProbeResult? _nat;

  bool _sheetOpen = false;
  bool _immersive = false;

  @override
  void initState() {
    super.initState();
    _loadTarget();
    _probeNat();
    _pollOnce();
    _poll = Timer.periodic(const Duration(seconds: 1), (_) => _pollOnce());
    _tick = Timer.periodic(const Duration(seconds: 1), (_) {
      if (mounted) setState(() {});
    });
    unawaited(_startVideo());
  }

  Timer? _videoTimer;

  /// ER-504b：当前不做自动重连；用户可手动重新 attach 同一会话。
  Future<void> _reconnectVideo() async {
    _videoTimer?.cancel();
    SessionVideo.instance.close();
    await _startVideo();
    if (mounted) setState(() {});
  }

  /// 接管会话并开始拉帧：FFI 里已经建好双缓冲，这里只是定时把最新帧解码成 ui.Image。
  Future<void> _startVideo() async {
    final token = widget.client.token ?? "";
    final deviceId = widget.client.deviceId ?? "";
    final deviceToken = widget.client.deviceToken ?? "";
    if (token.isEmpty || deviceId.isEmpty) {
      debugPrint("[video] 缺少登录态，跳过画面接管");
      return;
    }
    try {
      await widget.client.ensureRuntimeConfig();
    } catch (e) {
      debugPrint("[video] 运行时端点加载失败：$e");
      return;
    }
    final relay = widget.client.relayAddr;
    if (relay.isEmpty) {
      debugPrint("[video] 服务端未下发 relay 端点，跳过画面接管");
      return;
    }
    await SessionVideo.instance.attach(
      gateway: widget.client.gatewayURL,
      relay: relay,
      token: token,
      deviceId: deviceId,
      deviceToken: deviceToken,
      sessionId: widget.sessionId,
    );
    _videoTimer = Timer.periodic(const Duration(milliseconds: 60), (_) async {
      await SessionVideo.instance.poll();
      if (mounted && SessionVideo.instance.image != null) setState(() {});
    });
  }

  @override
  void dispose() {
    _videoTimer?.cancel();
    SessionVideo.instance.close();
    _poll?.cancel();
    _tick?.cancel();
    super.dispose();
  }

  Future<void> _probeNat() async {
    final result = await probeNatCached();
    if (mounted) setState(() => _nat = result);
  }

  Future<void> _loadTarget() async {
    final code = widget.targetCode;
    if (code.isEmpty) return;
    try {
      final devices = await widget.client.listDevices();
      if (!mounted) return;
      for (final d in devices) {
        if (d.code == code) {
          setState(() => _target = d);
          return;
        }
      }
    } catch (_) {
      // 目标设备平台信息是增强项：取不到就用「未知（需桌面端）」。
    }
  }

  Future<void> _pollOnce() async {
    if (_polling) return;
    _polling = true;
    try {
      final s = await widget.client.sessionStatus(widget.sessionId);
      if (!mounted) return;
      final wasLive = _status.isLive;
      setState(() => _status = s);
      if (!wasLive && s.isLive) {
        _liveSince = DateTime.now();
        _measureLink();
      }
      if (s.state == SessionState.ended || s.state == SessionState.failed) {
        _poll?.cancel();
      }
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(
        () => _status = SessionStatus(SessionState.failed, message: e.message),
      );
      _poll?.cancel();
    } finally {
      _polling = false;
    }
  }

  /// 链路采样：时延/丢包用真实 5 次 ping；出口地址在质量抽屉里单独采样。
  Future<void> _measureLink() async {
    final rtts = <Duration>[];
    var lost = 0;
    for (var i = 0; i < 5; i++) {
      final rtt = await widget.client.ping();
      if (rtt == null) {
        lost++;
      } else {
        rtts.add(rtt);
      }
      if (!mounted) return;
      setState(() {
        _samples = i + 1;
        _lost = lost;
        if (rtts.isNotEmpty) _rtt = _median(rtts);
      });
      await Future<void>.delayed(const Duration(milliseconds: 120));
    }
  }

  static Duration _median(List<Duration> values) {
    final sorted = [...values]..sort((a, b) => a.compareTo(b));
    return sorted[sorted.length ~/ 2];
  }

  String get _prettyCode {
    final code = widget.targetCode;
    if (code.length != 9) return code;
    return code.substring(0, 3) +
        ' ' +
        code.substring(3, 6) +
        ' ' +
        code.substring(6);
  }

  String get _ownCode {
    final own = widget.ownDevice;
    if (own != null && own.code.isNotEmpty) return own.prettyCode;
    final c = widget.client.ownDeviceCode;
    if (c.length == 9)
      return c.substring(0, 3) + ' ' + c.substring(3, 6) + ' ' + c.substring(6);
    return c.isEmpty ? '未知' : c;
  }

  /// 目标平台：查到真值就用真值。
  String get _platform {
    final p = _target?.platform ?? '';
    if (p.isEmpty || p == 'unknown') return '未知（需桌面端）';
    return p;
  }

  String get _stateLabel => switch (_status.state) {
    SessionState.requesting => '正在发起请求',
    SessionState.waitingConsent => '等待对方授权',
    SessionState.connecting => '正在建立连接',
    SessionState.authorized => '已授权 · 通道建立中',
    SessionState.active => '会话进行中',
    SessionState.ended => '会话已结束',
    SessionState.failed => '连接失败',
    SessionState.idle => '空闲',
  };

  /// 三步进度（原型 s04 的五步在 Web 侧只有前三步有真实含义，压缩为一行小字）。
  int get _stepIndex => switch (_status.state) {
    SessionState.requesting => 0,
    SessionState.waitingConsent => 1,
    _ => 2,
  };

  static const _stepLabels = ['发起连接请求', '对方授权', '建立加密通道'];

  String _fmtClock(Duration d) {
    final m = d.inMinutes.remainder(60).toString().padLeft(2, '0');
    final s = d.inSeconds.remainder(60).toString().padLeft(2, '0');
    if (d.inHours > 0) return d.inHours.toString() + ':' + m + ':' + s;
    return m + ':' + s;
  }

  String get _elapsed {
    final since = _liveSince;
    return _fmtClock(DateTime.now().difference(since ?? _openedAt));
  }

  void _openDiagnostics() {
    Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => Scaffold(body: DiagnosticsPage(client: widget.client)),
      ),
    );
  }

  void _leaveWith(String message) {
    ScaffoldMessenger.of(context)
        .showSnackBar(SnackBar(content: Text(message)));
    widget.onLeave();
  }

  @override
  Widget build(BuildContext context) {
    final live = _status.isLive;
    return Scaffold(
      backgroundColor: live ? const Color(0xFF23262B) : null,
      body: SafeArea(child: live ? _liveBody(context) : _waitingBody(context)),
    );
  }

  // ---------------------------------------------------------------- s04 等待授权

  Widget _waitingBody(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    final width = MediaQuery.sizeOf(context).width;
    final wide = width >= 1000;

    final card = _connectCard(context);
    final side = _infoCard(context);

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: EdgeInsets.fromLTRB(wide ? 18 : 8, 8, wide ? 24 : 8, 0),
          child: Row(
            children: [
              TextButton.icon(
                key: const Key('back-to-devices'),
                onPressed: widget.onLeave,
                icon: const Icon(Icons.chevron_left_rounded, size: 18),
                label: const Text('返回设备列表'),
              ),
              const Spacer(),
              Container(
                padding: const EdgeInsets.symmetric(
                  horizontal: 10,
                  vertical: 5,
                ),
                decoration: BoxDecoration(
                  color: scheme.surfaceContainerHigh,
                  borderRadius: BorderRadius.circular(ErTokens.radiusPill),
                ),
                child: Text(
                  '连接中 · 步骤 ' + (_stepIndex + 1).toString() + ' / 3',
                  style: text.labelSmall?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
                ),
              ),
            ],
          ),
        ),
        Expanded(
          child: SingleChildScrollView(
            padding: EdgeInsets.fromLTRB(
              wide ? 32 : 16,
              12,
              wide ? 32 : 16,
              28,
            ),
            child: wide
                ? Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Expanded(child: card),
                      const SizedBox(width: 20),
                      SizedBox(width: 320, child: side),
                    ],
                  )
                : Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [card, const SizedBox(height: 16), side],
                  ),
          ),
        ),
      ],
    );
  }

  Widget _connectCard(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    final failed = _status.state == SessionState.failed;

    return Container(
      key: const Key('waiting-card'),
      padding: const EdgeInsets.all(22),
      decoration: BoxDecoration(
        color: scheme.surfaceContainerLow,
        borderRadius: BorderRadius.circular(ErTokens.radiusXl),
        border: Border.all(color: scheme.outlineVariant.withValues(alpha: 0.7)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Container(
                width: 46,
                height: 46,
                decoration: BoxDecoration(
                  color: scheme.surfaceContainerHigh,
                  borderRadius: BorderRadius.circular(12),
                ),
                child: Icon(
                  Icons.desktop_windows_rounded,
                  size: 24,
                  color: scheme.onSurfaceVariant,
                ),
              ),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      _prettyCode.isEmpty ? '目标设备' : _prettyCode,
                      key: const Key('target-code'),
                      style: text.titleLarge?.copyWith(letterSpacing: 1.6),
                    ),
                    const SizedBox(height: 2),
                    Text(
                      _platform + ' · 位置未知（需桌面端）',
                      key: const Key('target-platform'),
                      style: text.bodySmall?.copyWith(
                        color: scheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                ),
              ),
              _OnlineChip(device: _target, connected: _status.isLive),
            ],
          ),
          const SizedBox(height: 18),
          _StepLine(step: _stepIndex, labels: _stepLabels, failed: failed),
          const SizedBox(height: 14),
          Row(
            children: [
              Icon(
                failed ? Icons.error_outline_rounded : Icons.lock_clock_rounded,
                size: 15,
                color: failed ? scheme.error : scheme.onSurfaceVariant,
              ),
              const SizedBox(width: 6),
              Expanded(
                child: Text(
                  switch (_status.state) {
                    SessionState.requesting => '已向服务端发起请求，正在等待被控设备出现在链路上。',
                    SessionState.waitingConsent => '已通知对方，请在对方电脑上确认；确认后自动进入会话。',
                    SessionState.connecting => '对方已确认，正在建立端到端加密通道。',
                    _ => _stateLabel,
                  },
                  style: text.bodySmall?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
                ),
              ),
              Text(
                '已等待 ' + _elapsed,
                style: text.labelSmall?.copyWith(
                  color: scheme.onSurfaceVariant,
                ),
              ),
            ],
          ),
          if (_status.message.isNotEmpty) ...[
            const SizedBox(height: 12),
            Container(
              padding: const EdgeInsets.all(12),
              decoration: BoxDecoration(
                color: scheme.errorContainer,
                borderRadius: BorderRadius.circular(ErTokens.radiusMd),
              ),
              child: Text(
                _status.message,
                style: text.bodySmall?.copyWith(color: scheme.onErrorContainer),
              ),
            ),
          ],
          const SizedBox(height: 20),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              OutlinedButton(
                key: const Key('cancel-connect'),
                onPressed: () => _leaveWith('已取消本次连接请求'),
                child: const Text('取消连接'),
              ),
              TextButton(
                key: const Key('run-diagnostics-waiting'),
                onPressed: _openDiagnostics,
                child: const Text('网络有问题？运行诊断'),
              ),
              Text(
                '连接全程端到端加密',
                style: text.labelSmall?.copyWith(
                  color: scheme.onSurfaceVariant,
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }

  Widget _infoCard(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;

    Widget row(String label, String value) => Padding(
      key: Key('info-' + label),
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 86,
            child: Text(
              label,
              style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant),
            ),
          ),
          Expanded(
            child: Text(
              value,
              style: text.bodySmall?.copyWith(fontWeight: FontWeight.w600),
            ),
          ),
        ],
      ),
    );

    return Container(
      key: const Key('connect-info-card'),
      padding: const EdgeInsets.fromLTRB(18, 18, 18, 16),
      decoration: BoxDecoration(
        color: scheme.surfaceContainerLow,
        borderRadius: BorderRadius.circular(ErTokens.radiusXl),
        border: Border.all(color: scheme.outlineVariant.withValues(alpha: 0.7)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('连接信息', style: text.titleSmall),
          const SizedBox(height: 8),
          row('本机设备码', _ownCode),
          row('连接方式', widget.assist ? '协助码临时授权' : '设备码直连'),
          row(
            '本机 NAT',
            _nat == null ? '探测中…' : (_nat!.supported ? _nat!.nat : '不支持（当前平台）'),
          ),
          row('中继节点', '未知（需桌面端）'),
          row('加密协议', 'Ed25519 握手 · 中继只见密文'),
          row('画质策略', '智能 · 自动（本机设置）'),
          const SizedBox(height: 10),
          Container(
            padding: const EdgeInsets.all(12),
            decoration: BoxDecoration(
              color: scheme.surfaceContainer,
              borderRadius: BorderRadius.circular(ErTokens.radiusMd),
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text('为什么需要对方确认？', style: text.labelMedium),
                const SizedBox(height: 4),
                Text(
                  '防止陌生人在你不知情时接入。被控端可在「设置 · 安全」中调整确认策略，但无人值守模式仍会保留验证码。',
                  style: text.labelSmall?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  // ---------------------------------------------------------------- s05 会话中

  Widget _liveBody(BuildContext context) {
    final width = MediaQuery.sizeOf(context).width;
    final showBadge = width >= 760;

    return Stack(
      children: [
        Positioned.fill(child: _remoteView(context)),
        if (!_immersive)
          Positioned(
            top: 0,
            left: 0,
            right: 0,
            child: _sessionTopBar(context, width),
          ),
        if (showBadge && !_immersive)
          Positioned(
            top: 64,
            right: 16,
            child: _QualityBadge(rtt: _rtt, samples: _samples, lost: _lost),
          ),
        Align(
          alignment: Alignment.bottomCenter,
          child: Padding(
            padding: const EdgeInsets.only(bottom: 22),
            child: _sessionToolbar(context, width),
          ),
        ),
        if (_sheetOpen)
          Positioned.fill(
            child: _QualityDrawer(
              client: widget.client,
              sessionId: widget.sessionId,
              targetLabel: _prettyCode.isEmpty
                  ? '会话 ' + widget.sessionId
                  : _prettyCode,
              stateLabel: _stateLabel,
              connectedFor: _elapsed,
              onClose: () => setState(() => _sheetOpen = false),
              onDiagnostics: _openDiagnostics,
            ),
          ),
      ],
    );
  }

  /// 远端画面：有帧就画，没帧才显示占位说明。
  Widget _remoteView(BuildContext context) {
    final img = SessionVideo.instance.image;
    if (img == null) {
      // 把 FFI 侧的错误直接显示出来：黑屏时这是唯一能自证的线索
      return _remotePlaceholder(context, SessionVideo.instance.error);
    }
    return Container(
      key: const Key("remote-canvas"),
      color: Colors.black,
      child: LayoutBuilder(
        builder: (context, c) {
          final iw = img.width.toDouble();
          final ih = img.height.toDouble();
          final scale = (c.maxWidth / iw).clamp(0.0, c.maxHeight / ih);
          return Center(
            child: Listener(
              onPointerMove: (e) => _sendPointer(e.localPosition, scale, img),
              onPointerDown: (e) {
                _sendPointer(e.localPosition, scale, img);
                SessionVideo.instance.send(1, 0, 1);
              },
              onPointerUp: (e) => SessionVideo.instance.send(1, 0, 0),
              child: SizedBox(
                width: iw * scale,
                height: ih * scale,
                child: SessionVideo.instance.textureId != null
                    ? Texture(textureId: SessionVideo.instance.textureId!)
                    : RawImage(image: img, fit: BoxFit.fill),
              ),
            ),
          );
        },
      ),
    );
  }

  /// 把窗口坐标换算成被控端像素坐标再发过去。
  void _sendPointer(Offset local, double scale, ui.Image img) {
    final x = (local.dx / scale).clamp(0.0, img.width.toDouble()).round();
    final y = (local.dy / scale).clamp(0.0, img.height.toDouble()).round();
    SessionVideo.instance.send(0, x, y);
  }

  Widget _remotePlaceholder(BuildContext context, [String error = '']) {
    return Container(
      key: const Key('remote-canvas'),
      decoration: const BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [Color(0xFF2A2E34), Color(0xFF1B1E22)],
        ),
      ),
      alignment: Alignment.center,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            width: 96,
            height: 96,
            decoration: BoxDecoration(
              color: Colors.white10,
              borderRadius: BorderRadius.circular(24),
              border: Border.all(color: Colors.white24),
            ),
            child: const Icon(
              Icons.desktop_windows_rounded,
              size: 46,
              color: Colors.white54,
            ),
          ),
          const SizedBox(height: 18),
          const Text(
            '桌面端渲染远端画面',
            style: TextStyle(
              fontSize: 16,
              fontWeight: FontWeight.w600,
              color: Colors.white70,
            ),
          ),
          const SizedBox(height: 6),
          Text(
            error.isEmpty
                ? '等待被控端画面…（若长时间无画面，请确认对方已开启「允许被控」）'
                : '画面接管失败：' + error,
            textAlign: TextAlign.center,
            style: TextStyle(
              fontSize: 12,
              color: error.isEmpty ? Colors.white38 : Colors.orangeAccent,
            ),
          ),
        ],
      ),
    );
  }

  Widget _sessionTopBar(BuildContext context, double width) {
    return Container(
      padding: const EdgeInsets.fromLTRB(8, 8, 16, 14),
      decoration: const BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topCenter,
          end: Alignment.bottomCenter,
          colors: [Color(0xCC000000), Color(0x00000000)],
        ),
      ),
      child: Row(
        children: [
          IconButton(
            tooltip: '返回设备列表',
            onPressed: widget.onLeave,
            icon: const Icon(Icons.chevron_left_rounded, color: Colors.white70),
          ),
          const SizedBox(width: 2),
          Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: [
              Text(
                _prettyCode.isEmpty ? '远程会话' : _prettyCode,
                style: const TextStyle(
                  fontSize: 13,
                  fontWeight: FontWeight.w600,
                  color: Colors.white,
                ),
              ),
              Text(
                '已连接 ' + _elapsed,
                style: const TextStyle(fontSize: 11, color: Colors.white54),
              ),
            ],
          ),
          const Spacer(),
          Container(
            key: const Key('live-state-chip'),
            padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 5),
            decoration: BoxDecoration(
              color: Colors.white.withValues(alpha: 0.12),
              borderRadius: BorderRadius.circular(ErTokens.radiusPill),
              border: Border.all(color: Colors.white24),
            ),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Container(
                  width: 6,
                  height: 6,
                  decoration: BoxDecoration(
                    color: _status.state == SessionState.active
                        ? const Color(0xFF6BE39B)
                        : const Color(0xFFFFC46B),
                    shape: BoxShape.circle,
                  ),
                ),
                const SizedBox(width: 6),
                Text(
                  _stateLabel,
                  style: const TextStyle(fontSize: 11, color: Colors.white70),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _sessionToolbar(BuildContext context, double width) {
    final compact = width < 560;
    return Container(
      key: const Key('session-toolbar'),
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
      decoration: BoxDecoration(
        color: const Color(0xF01F2226),
        borderRadius: BorderRadius.circular(ErTokens.radiusPill),
        border: Border.all(color: Colors.white12),
        boxShadow: const [
          BoxShadow(
            color: Color(0x55000000),
            blurRadius: 18,
            offset: Offset(0, 6),
          ),
        ],
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          _ToolItem(
            key: const Key('quality-toggle'),
            icon: Icons.tune_rounded,
            label: '质量详情',
            compact: compact,
            onTap: () => setState(() => _sheetOpen = true),
          ),
          _ToolItem(
            key: const Key('fullscreen-toggle'),
            icon: _immersive
                ? Icons.fullscreen_exit_rounded
                : Icons.fullscreen_rounded,
            label: _immersive ? '退出全屏' : '全屏',
            compact: compact,
            onTap: () => setState(() => _immersive = !_immersive),
          ),
          if (!compact)
            const Padding(
              padding: EdgeInsets.symmetric(horizontal: 6),
              child: SizedBox(
                height: 18,
                child: VerticalDivider(
                  width: 1,
                  color: Colors.white24,
                  thickness: 1,
                ),
              ),
            ),
          _ToolItem(
            key: const Key('reconnect'),
            icon: Icons.refresh_rounded,
            label: '重连',
            compact: compact,
            onTap: _reconnectVideo,
          ),
          _ToolItem(
            key: const Key('disconnect'),
            icon: Icons.call_end_rounded,
            label: '断开连接',
            compact: compact,
            danger: true,
            onTap: () => _leaveWith('已断开连接'),
          ),
        ],
      ),
    );
  }
}

// ---------------------------------------------------------------- 小组件

/// 在线徽标：只有真查到设备才敢写「在线 / 离线」，否则如实标「待确认」。
class _OnlineChip extends StatelessWidget {
  const _OnlineChip({required this.device, required this.connected});

  final DeviceInfo? device;
  final bool connected;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    final device = this.device;
    final (Color bg, Color fg, String label) = connected
        ? (const Color(0xFFC8F6D0), const Color(0xFF2A6239), '在线')
        : device == null
        ? (scheme.surfaceContainerHigh, scheme.onSurfaceVariant, '在线状态待确认')
        : device.online
        ? (const Color(0xFFC8F6D0), const Color(0xFF2A6239), '在线')
        : (scheme.surfaceContainerHigh, scheme.onSurfaceVariant, '离线');
    return Container(
      key: const Key('target-online-chip'),
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 5),
      decoration: BoxDecoration(
        color: bg,
        borderRadius: BorderRadius.circular(ErTokens.radiusPill),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            width: 6,
            height: 6,
            decoration: BoxDecoration(color: fg, shape: BoxShape.circle),
          ),
          const SizedBox(width: 6),
          Text(
            label,
            style: text.labelSmall?.copyWith(
              color: fg,
              fontWeight: FontWeight.w600,
            ),
          ),
        ],
      ),
    );
  }
}

/// 三步进度：原型 s04 的五步只保留与 Web 侧真实能力相符的三步，压成一行小字。
class _StepLine extends StatelessWidget {
  const _StepLine({
    required this.step,
    required this.labels,
    required this.failed,
  });

  final int step;
  final List<String> labels;
  final bool failed;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    return Wrap(
      key: const Key('step-line'),
      spacing: 10,
      runSpacing: 4,
      crossAxisAlignment: WrapCrossAlignment.center,
      children: [
        for (var i = 0; i < labels.length; i++)
          Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              if (i > 0)
                Padding(
                  padding: const EdgeInsets.only(right: 6),
                  child: Icon(
                    Icons.chevron_right_rounded,
                    size: 14,
                    color: scheme.outlineVariant,
                  ),
                ),
              Icon(
                i < step
                    ? Icons.check_circle_rounded
                    : i == step
                    ? (failed
                          ? Icons.error_rounded
                          : Icons.radio_button_checked_rounded)
                    : Icons.circle_outlined,
                size: 14,
                color: failed && i == step
                    ? scheme.error
                    : i <= step
                    ? scheme.primary
                    : scheme.outlineVariant,
              ),
              const SizedBox(width: 4),
              Text(
                labels[i],
                style: text.labelSmall?.copyWith(
                  color: i <= step ? scheme.onSurface : scheme.onSurfaceVariant,
                  fontWeight: i == step ? FontWeight.w600 : FontWeight.w400,
                ),
              ),
            ],
          ),
      ],
    );
  }
}

class _ToolItem extends StatelessWidget {
  const _ToolItem({
    super.key,
    required this.icon,
    required this.label,
    required this.onTap,
    this.compact = false,
    this.danger = false,
  });

  final IconData icon;
  final String label;
  final VoidCallback onTap;
  final bool compact;
  final bool danger;

  @override
  Widget build(BuildContext context) {
    final color = danger ? const Color(0xFFFF8A80) : Colors.white70;
    return TextButton(
      onPressed: onTap,
      style: TextButton.styleFrom(
        foregroundColor: color,
        minimumSize: const Size(0, 40),
        padding: EdgeInsets.symmetric(horizontal: compact ? 10 : 14),
        shape: const StadiumBorder(),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 18, color: color),
          if (!compact) ...[
            const SizedBox(width: 6),
            Text(
              label,
              style: TextStyle(
                fontSize: 12.5,
                fontWeight: FontWeight.w600,
                color: color,
              ),
            ),
          ],
        ],
      ),
    );
  }
}

/// 常驻质量浮层（原型 s05 右上角）。
class _QualityBadge extends StatelessWidget {
  const _QualityBadge({
    required this.rtt,
    required this.samples,
    required this.lost,
  });

  final Duration? rtt;
  final int samples;
  final int lost;

  @override
  Widget build(BuildContext context) {
    Widget item(String label, String value, {String? unit}) => Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          label,
          style: const TextStyle(fontSize: 10, color: Colors.white38),
        ),
        const SizedBox(height: 2),
        RichText(
          text: TextSpan(
            text: value,
            style: const TextStyle(
              fontSize: 15,
              fontWeight: FontWeight.w600,
              color: Colors.white,
            ),
            children: [
              if (unit != null)
                TextSpan(
                  text: unit,
                  style: const TextStyle(
                    fontSize: 10,
                    fontWeight: FontWeight.w400,
                    color: Colors.white38,
                  ),
                ),
            ],
          ),
        ),
      ],
    );

    final lossRate = samples == 0 ? 0.0 : lost / samples * 100;
    final lossText = samples == 0
        ? '正在采样…'
        : (lost == 0 ? '网络稳定 · ' : '有丢包 · ') +
              lossRate.toStringAsFixed(0) +
              '% 丢包';
    return Container(
      key: const Key('quality-badge'),
      width: 214,
      padding: const EdgeInsets.fromLTRB(14, 12, 14, 12),
      decoration: BoxDecoration(
        color: const Color(0xE61F2226),
        borderRadius: BorderRadius.circular(ErTokens.radiusLg),
        border: Border.all(color: Colors.white12),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              const Text(
                '连接质量',
                style: TextStyle(fontSize: 11, color: Colors.white54),
              ),
              const Spacer(),
              const Text(
                '本机采样',
                style: TextStyle(fontSize: 11, color: Colors.white70),
              ),
            ],
          ),
          const SizedBox(height: 10),
          Row(
            children: [
              Expanded(child: item('帧率', '—', unit: '未接入')),
              Expanded(
                child: item(
                  '网关 RTT',
                  rtt == null ? '—' : rtt!.inMilliseconds.toString(),
                  unit: 'ms',
                ),
              ),
            ],
          ),
          const SizedBox(height: 8),
          Row(
            children: [
              Expanded(child: item('码率', '—', unit: '未接入')),
              Expanded(child: item('链路类型', '未接入')),
            ],
          ),
          const SizedBox(height: 10),
          Row(
            children: [
              Container(
                width: 6,
                height: 6,
                decoration: BoxDecoration(
                  color: samples == 0
                      ? Colors.white38
                      : lost == 0
                      ? const Color(0xFF6BE39B)
                      : const Color(0xFFFFC46B),
                  shape: BoxShape.circle,
                ),
              ),
              const SizedBox(width: 6),
              Expanded(
                child: Text(
                  samples == 0 ? '正在采样…' : lossText,
                  style: const TextStyle(fontSize: 10.5, color: Colors.white54),
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

/// s06 质量详情抽屉：RTT/丢包取真实 ping，码率/帧率标「桌面端」。
class _QualityDrawer extends StatefulWidget {
  const _QualityDrawer({
    required this.client,
    required this.sessionId,
    required this.targetLabel,
    required this.stateLabel,
    required this.connectedFor,
    required this.onClose,
    required this.onDiagnostics,
  });

  final GatewayClient client;
  final String sessionId;
  final String targetLabel;
  final String stateLabel;
  final String connectedFor;
  final VoidCallback onClose;
  final VoidCallback onDiagnostics;

  @override
  State<_QualityDrawer> createState() => _QualityDrawerState();
}

class _QualityDrawerState extends State<_QualityDrawer> {
  final List<Duration> _rtts = <Duration>[];
  int _total = 0;
  int _lost = 0;
  ReflectInfo? _reflect;
  NatProbeResult? _nat;
  Timer? _periodic;
  bool _sampling = false;

  @override
  void initState() {
    super.initState();
    _measure();
    _probeNat();
    // 原型写「数据每 2 秒刷新」：这里每 2 秒补一次真实 ping，滚动窗口 5 个。
    _periodic = Timer.periodic(const Duration(seconds: 2), (_) => _sample());
  }

  @override
  void dispose() {
    _periodic?.cancel();
    super.dispose();
  }

  Future<void> _measure() async {
    if (_sampling) return;
    _sampling = true;
    try {
      final info = await widget.client.netReflect();
      if (!mounted) return;
      setState(() => _reflect = info);
    } catch (_) {
      // 出口地址取不到不影响时延统计。
    }
    for (var i = 0; i < 5; i++) {
      await _sample();
      if (!mounted) return;
      await Future<void>.delayed(const Duration(milliseconds: 120));
    }
    _sampling = false;
  }

  Future<void> _probeNat() async {
    final result = await probeNatCached();
    if (mounted) setState(() => _nat = result);
  }

  Future<void> _sample() async {
    _total++;
    final rtt = await widget.client.ping();
    if (!mounted) return;
    setState(() {
      if (rtt == null) {
        _lost++;
      } else {
        _rtts.add(rtt);
        if (_rtts.length > 5) _rtts.removeAt(0);
      }
    });
  }

  Duration? get _median {
    if (_rtts.isEmpty) return null;
    final sorted = [..._rtts]..sort((a, b) => a.compareTo(b));
    return sorted[sorted.length ~/ 2];
  }

  double get _loss => _total == 0 ? 0 : _lost / _total * 100;

  String get _report {
    final m = _median;
    return [
      'EasyRemote 连接质量报告',
      '设备：' + widget.targetLabel,
      '会话：' +
          widget.sessionId +
          '（' +
          widget.stateLabel +
          '，已连接 ' +
          widget.connectedFor +
          '）',
      '采样：' +
          _total.toString() +
          ' 次 · 中位 RTT ' +
          (m == null ? '—' : m.inMilliseconds.toString() + ' ms') +
          ' · 丢包 ' +
          _loss.toStringAsFixed(0) +
          '%',
      '出口地址（TCP）：' + (_reflect?.pretty ?? '—'),
      '帧率 / 码率 / 编码器：需桌面端解码统计，Web 预览不提供',
      '生成时间：' + DateTime.now().toIso8601String(),
    ].join('\n');
  }

  Future<void> _copy() async {
    await Clipboard.setData(ClipboardData(text: _report));
    if (!mounted) return;
    ScaffoldMessenger.of(context)
        .showSnackBar(const SnackBar(content: Text('质量报告已复制到剪贴板')));
  }

  @override
  Widget build(BuildContext context) {
    final width = MediaQuery.sizeOf(context).width;
    final panelWidth = width >= 760 ? 460.0 : width;
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    final median = _median;
    final video = SessionVideo.instance;
    final mediaFps = video.presentFps > 0 ? video.presentFps : video.nativeFps;
    final dataLoss = video.attached ? video.lossPct : null;

    return Stack(
      children: [
        Positioned.fill(
          child: GestureDetector(
            key: const Key('quality-scrim'),
            onTap: widget.onClose,
            child: Container(color: Colors.black54),
          ),
        ),
        Align(
          alignment: Alignment.centerRight,
          child: TweenAnimationBuilder<double>(
            tween: Tween(begin: 1, end: 0),
            duration: const Duration(milliseconds: 180),
            curve: Curves.easeOutCubic,
            builder: (context, value, child) => FractionalTranslation(
              translation: Offset(value, 0),
              child: child,
            ),
            child: Container(
              key: const Key('quality-sheet'),
              width: panelWidth,
              height: double.infinity,
              decoration: BoxDecoration(
                color: scheme.surface,
                borderRadius: const BorderRadius.only(
                  topLeft: Radius.circular(ErTokens.radiusXl),
                  bottomLeft: Radius.circular(ErTokens.radiusXl),
                ),
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Padding(
                    padding: const EdgeInsets.fromLTRB(20, 18, 12, 10),
                    child: Row(
                      children: [
                        Container(
                          width: 42,
                          height: 42,
                          decoration: BoxDecoration(
                            color: scheme.primaryContainer,
                            borderRadius: BorderRadius.circular(11),
                          ),
                          child: Icon(
                            Icons.monitor_heart_rounded,
                            size: 22,
                            color: scheme.onPrimaryContainer,
                          ),
                        ),
                        const SizedBox(width: 12),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text('连接质量', style: text.titleLarge),
                              Text(
                                widget.targetLabel +
                                    ' · ' +
                                    widget.stateLabel +
                                    ' · 已连接 ' +
                                    widget.connectedFor,
                                style: text.labelSmall?.copyWith(
                                  color: scheme.onSurfaceVariant,
                                ),
                              ),
                            ],
                          ),
                        ),
                        IconButton(
                          key: const Key('quality-close'),
                          tooltip: '关闭面板',
                          onPressed: widget.onClose,
                          icon: const Icon(Icons.close_rounded, size: 20),
                        ),
                      ],
                    ),
                  ),
                  const Divider(height: 1),
                  Expanded(
                    child: ListView(
                      padding: const EdgeInsets.fromLTRB(20, 16, 20, 16),
                      children: [
                        LayoutBuilder(
                          builder: (context, constraints) {
                            final two = constraints.maxWidth >= 380;
                            final w = two
                                ? (constraints.maxWidth - 12) / 2
                                : constraints.maxWidth;
                            return Wrap(
                              spacing: 12,
                              runSpacing: 12,
                              children: [
                                SizedBox(
                                  width: w,
                                  child: _MetricCard(
                                    key: const Key('metric-fps'),
                                    label: '帧率',
                                    value: mediaFps <= 0
                                        ? '—'
                                        : mediaFps.toString(),
                                    unit: 'fps',
                                    hint: mediaFps <= 0
                                        ? '等待首帧/未解码'
                                        : 'present/native 统计（' +
                                              video.decodeMs.toString() +
                                              ' ms/帧）',
                                    icon: Icons.speed_rounded,
                                  ),
                                ),
                                SizedBox(
                                  width: w,
                                  child: _MetricCard(
                                    key: const Key('metric-rtt'),
                                    label: '网关往返时延（RTT）',
                                    value: median == null
                                        ? '—'
                                        : median.inMilliseconds.toString(),
                                    unit: 'ms',
                                    hint: _total == 0
                                        ? '正在采样'
                                        : _total.toString() + ' 次网关探测中位数（非端到端）',
                                    icon: Icons.schedule_rounded,
                                    spark: _rtts
                                        .map((d) => d.inMilliseconds.toDouble())
                                        .toList(),
                                  ),
                                ),
                                SizedBox(
                                  width: w,
                                  child: _MetricCard(
                                    key: const Key('metric-bitrate'),
                                    label: '码率',
                                    value: video.attached
                                        ? (video.bitrateKbps / 1000)
                                              .toStringAsFixed(1)
                                        : '—',
                                    unit: 'Mbps',
                                    hint: video.attached
                                        ? '最近 1 秒数据面接收'
                                        : '等待数据面统计',
                                    icon: Icons.upload_rounded,
                                  ),
                                ),
                                SizedBox(
                                  width: w,
                                  child: _MetricCard(
                                    key: const Key('metric-loss'),
                                    label: '丢包率',
                                    value: (dataLoss ?? _loss).toStringAsFixed(
                                      1,
                                    ),
                                    unit: '%',
                                    hint: dataLoss != null
                                        ? '数据面分片丢失（最近 1 秒）'
                                        : (_total == 0
                                              ? '正在采样'
                                              : _lost.toString() +
                                                    ' / ' +
                                                    _total.toString() +
                                                    ' 次探测失败'),
                                    icon: Icons.wifi_rounded,
                                    warned: (dataLoss ?? _loss) > 0,
                                  ),
                                ),
                              ],
                            );
                          },
                        ),
                        const SizedBox(height: 18),
                        Text('链路明细', style: text.titleSmall),
                        const SizedBox(height: 6),
                        _detail(
                          '往返时延 RTT',
                          median == null
                              ? '—'
                              : median.inMilliseconds.toString() + ' ms',
                        ),
                        _detail('出口地址（TCP）', _reflect?.pretty ?? '未知'),
                        _detail(
                          '丢包率',
                          (dataLoss ?? _loss).toStringAsFixed(1) + ' %',
                        ),
                        _detail(
                          '输入 ACK P95',
                          video.attached
                              ? video.inputP95Ms.toString() +
                                    ' ms（' +
                                    video.inputAcks.toString() +
                                    ' 次）'
                              : '—',
                        ),
                        _detail(
                          '解码耗时',
                          video.attached
                              ? video.decodeMs.toString() + ' ms'
                              : '—',
                        ),
                        _detail(
                          '呈现帧率 / native 帧率',
                          mediaFps <= 0
                              ? '—'
                              : video.presentFps.toString() +
                                    ' / ' +
                                    video.nativeFps.toString() +
                                    ' fps',
                        ),
                        _detail(
                          'NAT 类型',
                          _nat == null
                              ? '探测中…'
                              : (_nat!.supported ? _nat!.nat : '不支持（当前平台）'),
                        ),
                        _detail('当前档位', '智能 · 自动（本机设置）'),
                        const SizedBox(height: 10),
                        Text(
                          '丢包在数据面可用时取最近 1 秒分片丢失率；否则退回网关 ping 探测。帧率/码率/解码耗时来自数据面 telemetry（native/decoder 统计），不是 UI 轮询次数。',
                          style: text.labelSmall?.copyWith(
                            color: scheme.onSurfaceVariant,
                            height: 1.6,
                          ),
                        ),
                      ],
                    ),
                  ),
                  const Divider(height: 1),
                  Padding(
                    padding: const EdgeInsets.fromLTRB(20, 12, 20, 16),
                    child: Row(
                      children: [
                        Expanded(
                          child: OutlinedButton.icon(
                            key: const Key('run-diagnostics'),
                            onPressed: widget.onDiagnostics,
                            icon: const Icon(
                              Icons.monitor_heart_outlined,
                              size: 16,
                            ),
                            label: const Text('运行网络诊断'),
                          ),
                        ),
                        const SizedBox(width: 10),
                        Expanded(
                          child: FilledButton.icon(
                            key: const Key('copy-report'),
                            onPressed: _copy,
                            icon: const Icon(Icons.copy_rounded, size: 16),
                            label: const Text('复制质量报告'),
                          ),
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ],
    );
  }

  Widget _detail(String label, String value) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 5),
      child: Row(
        children: [
          Expanded(
            child: Text(
              label,
              style: text.bodySmall?.copyWith(color: scheme.onSurfaceVariant),
            ),
          ),
          Text(
            value,
            style: text.bodySmall?.copyWith(fontWeight: FontWeight.w600),
          ),
        ],
      ),
    );
  }
}

class _MetricCard extends StatelessWidget {
  const _MetricCard({
    super.key,
    required this.label,
    required this.value,
    required this.unit,
    required this.hint,
    required this.icon,
    this.spark = const <double>[],
    this.warned = false,
  });

  final String label;
  final String value;
  final String unit;
  final String hint;
  final IconData icon;
  final List<double> spark;
  final bool warned;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    final maxV = spark.isEmpty ? 1.0 : spark.reduce((a, b) => a > b ? a : b);

    return Container(
      padding: const EdgeInsets.fromLTRB(14, 12, 14, 12),
      decoration: BoxDecoration(
        color: scheme.surfaceContainerLow,
        borderRadius: BorderRadius.circular(ErTokens.radiusLg),
        border: Border.all(
          color: warned
              ? scheme.error.withValues(alpha: 0.4)
              : scheme.outlineVariant.withValues(alpha: 0.7),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(icon, size: 14, color: scheme.onSurfaceVariant),
              const SizedBox(width: 6),
              Text(
                label,
                style: text.labelSmall?.copyWith(
                  color: scheme.onSurfaceVariant,
                ),
              ),
            ],
          ),
          const SizedBox(height: 6),
          RichText(
            text: TextSpan(
              text: value,
              style: text.headlineSmall?.copyWith(
                fontFeatures: const [FontFeature.tabularFigures()],
              ),
              children: [
                TextSpan(
                  text: unit,
                  style: text.labelSmall?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(height: 4),
          Text(
            hint,
            style: text.labelSmall?.copyWith(
              color: scheme.onSurfaceVariant,
              fontSize: 10,
            ),
          ),
          if (spark.isNotEmpty) ...[
            const SizedBox(height: 8),
            SizedBox(
              height: 22,
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.end,
                children: [
                  for (final v in spark)
                    Expanded(
                      child: Padding(
                        padding: const EdgeInsets.symmetric(horizontal: 1),
                        child: Container(
                          height: (22 * (v / maxV)).clamp(3, 22),
                          decoration: BoxDecoration(
                            color: scheme.primary.withValues(alpha: 0.55),
                            borderRadius: BorderRadius.circular(2),
                          ),
                        ),
                      ),
                    ),
                ],
              ),
            ),
          ],
        ],
      ),
    );
  }
}
