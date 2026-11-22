import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../api.dart';
import '../capabilities.dart';
import '../consent_controller.dart';
import '../consent_dialog.dart';
import '../host_service.dart';
import '../local_control_bar.dart';

import '../models.dart';
import '../prefs.dart';
import '../shell.dart';
import '../theme.dart';

/// 设备页：本机设备码 + 已绑定设备卡片（复刻原型「首页 · 设备列表」）。
class DeviceListPage extends StatefulWidget {
  const DeviceListPage({
    super.key,
    required this.client,
    required this.ownDevice,
    required this.onConnect,
    this.onSessionStarted,
  });

  final GatewayClient client;
  final DeviceInfo ownDevice;
  final ValueChanged<String> onConnect;

  /// 同账号免密直连成功后直接打开会话页（不经过连接页）。
  final void Function(String sessionId, String targetCode)? onSessionStarted;

  @override
  State<DeviceListPage> createState() => _DeviceListPageState();
}

enum _Filter { all, online, offline }

class _DeviceListPageState extends State<DeviceListPage> {
  late Future<List<DeviceInfo>> _future;
  final _search = TextEditingController();
  _Filter _filter = _Filter.all;

  // 被控（「允许被控」开关）：值全来自 HostService，UI 只做展示。
  bool _hostOn = false;
  bool _hostBusy = false;
  String? _hostPassword;
  bool _showPw = false;
  String _hostError = "";
  HostStatus _hostStatus = HostStatus.idle;
  Timer? _hostTimer;
  String? _directBusyCode;
  bool _consentDialogOpen = false;

  @override
  void initState() {
    super.initState();
    _future = widget.client.listDevices();
    // 偏好（免密直连）改了要立刻反映到设备卡片上的按钮文案。
    appPrefs.addListener(_onPrefs);
    // 本地授权请求：HostService 轮询到后写入 ConsentController，这里负责弹窗。
    ConsentController.instance.addListener(_onConsentChanged);
    // 被控状态每 2 秒轮询一次（主控连进来后 frames/injected 会变）
    _hostTimer = Timer.periodic(const Duration(seconds: 2), (_) {
      if (!mounted || !_hostOn) return;
      final st = HostService.instance.status();
      if (st.running != _hostStatus.running ||
          st.sessionId != _hostStatus.sessionId ||
          st.frames != _hostStatus.frames ||
          st.injected != _hostStatus.injected) {
        setState(() => _hostStatus = st);
      }
    });
    // 只在「本机已经被控服务持有」时同步展示状态；绝不因为登录/默认偏好自动开启。
    // C18 本地授权弹窗没实现前，被控能力在台账里是 disabled（见 capabilities.yaml）。
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      if (HostService.instance.active) {
        setState(() {
          _hostOn = true;
          _hostPassword = HostService.instance.password;
        });
      }
    });
  }

  void _onPrefs() {
    if (mounted) setState(() {});
  }

  Future<void> _disconnectRemote() async {
    final id = _hostStatus.sessionId;
    if (id.isNotEmpty) HostService.instance.closeSession(id);
    setState(() => _hostStatus = HostStatus.idle);
  }

  void _onConsentChanged() {
    if (!mounted) return;
    final controller = ConsentController.instance;
    if (controller.pending != null && !_consentDialogOpen) {
      _consentDialogOpen = true;
      showDialog<void>(
        context: context,
        barrierDismissible: false,
        builder: (_) => ConsentDialog(controller: controller),
      ).whenComplete(() => _consentDialogOpen = false);
    } else if (controller.pending == null && _consentDialogOpen) {
      // 决策后 controller 会清空 pending；关闭对应弹窗（只关我们打开的那一个）。
      _consentDialogOpen = false;
      Navigator.of(context).maybePop();
    }
  }

  /// 「允许被控」开关：prepare（复用本机设备身份）→ 生成/同步密码 → 开流。
  ///
  /// [fromUser] 为 false 表示启动时的自动开启：失败只提示，不改用户偏好。
  Future<void> _toggleHost(bool on, {bool fromUser = true}) async {
    if (_hostBusy) return;
    final gate = CapabilityGate.instance.stateOf('remote_host');
    if (!gate.available) {
      setState(() {
        _hostOn = false;
        _hostError = "未接入：${gate.reason}";
      });
      return;
    }
    setState(() {
      _hostBusy = true;
      _hostError = "";
    });
    try {
      if (on) {
        if (!HostService.supported) {
          throw StateError("当前平台不支持被控：浏览器预览没有原生采集/注入，请在桌面客户端开启");
        }
        // 历史快照可能缺少 device_id（先落盘后绑设备），直接 prepare 只会得到
        // Rust 的 missing session。这里先用令牌把设备身份补齐。
        final device = await widget.client.ensureDevice(
          platform: currentPlatformName(),
        );
        final relay = widget.client.relayAddr;
        if (relay.isEmpty) {
          throw StateError("运行时端点未加载：无法启动被控（请检查服务端配置）");
        }
        final code = HostService.instance.prepare(
          gateway: widget.client.gatewayURL,
          relay: relay,
          token: widget.client.token ?? "",
          deviceId: device.id,
          deviceCode: device.code,
          deviceToken: widget.client.deviceToken ?? "",
        );
        final pw = HostService.instance.ensurePassword();
        HostService.instance.start(
          signalBase: widget.client.signalURL,
          keepAwake: appPrefs.value.preventSleep,
        );
        setState(() {
          _hostOn = true;
          _hostPassword = pw;
          _hostStatus = HostStatus.idle;
        });
        if (fromUser) _patchAllowControl(true);
        debugPrint("[host] 允许被控已开启 device_code=$code password=$pw");
      } else {
        HostService.instance.stop();
        setState(() {
          _hostOn = false;
          _hostStatus = HostStatus.idle;
        });
        if (fromUser) _patchAllowControl(false);
      }
    } catch (e) {
      setState(() {
        _hostOn = false;
        _hostError = _friendlyHostError(e);
      });
    } finally {
      if (mounted) setState(() => _hostBusy = false);
    }
  }

  void _patchAllowControl(bool on) {
    updatePrefs(appPrefs.value.copyWith(allowControl: on));
  }

  String _friendlyHostError(Object e) {
    final text = e.toString();
    if (text.contains('missing session') ||
        text.contains('AUTH_TOKEN_EXPIRED')) {
      return '登录态或本机设备绑定信息缺失：已尝试自动补绑，请重试；仍失败请重新登录。';
    }
    if (text.contains('er_ffi library not found')) {
      return '缺少本机核心库（er_ffi）：请先构建 core，或设置 ER_CORE_LIB 指向 liber_ffi。';
    }
    return text;
  }

  /// 同账号免密直连：点设备卡片的「连接」直接发起会话。
  Future<void> _directConnect(DeviceInfo d) async {
    if (_directBusyCode != null) return;
    final gate = CapabilityGate.instance.stateOf('remote_control');
    if (!gate.available) {
      ScaffoldMessenger.of(context)
          .showSnackBar(SnackBar(content: Text('远程控制未开放：${gate.reason}')));
      return;
    }
    setState(() => _directBusyCode = d.code);
    try {
      final sid = await widget.client.requestConnect(d.code, direct: true);
      if (!mounted) return;
      if (sid.isEmpty) {
        ScaffoldMessenger.of(context)
            .showSnackBar(const SnackBar(content: Text('服务端未返回会话 ID，请重试')));
        return;
      }
      final open = widget.onSessionStarted;
      if (open != null) {
        open(sid, d.code);
      } else {
        widget.onConnect(d.code);
      }
    } on ApiException catch (e) {
      if (!mounted) return;
      final hint = e.code.contains('CAPTCHA')
          ? '该设备不属于当前账号，请关闭免密直连或输入对方临时密码'
          : e.message;
      ScaffoldMessenger.of(context)
          .showSnackBar(SnackBar(content: Text('连接失败：' + hint)));
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context)
          .showSnackBar(SnackBar(content: Text('连接失败：' + e.toString())));
    } finally {
      if (mounted) setState(() => _directBusyCode = null);
    }
  }

  @override
  void dispose() {
    appPrefs.removeListener(_onPrefs);
    ConsentController.instance.removeListener(_onConsentChanged);
    _hostTimer?.cancel();
    // 只有应用真的销毁这个页面才停被控；切导航由 IndexedStack 保留页面，不会走到这里。
    if (_hostOn) HostService.instance.stop();
    _search.dispose();
    super.dispose();
  }

  void _reload() {
    setState(() => _future = widget.client.listDevices());
  }

  void _copy(String text) {
    Clipboard.setData(ClipboardData(text: text));
    ScaffoldMessenger.of(context)
        .showSnackBar(SnackBar(content: Text('已复制 ' + text)));
  }

  /// 生成一次性协助码（6 位、10 分钟、一码一用）。
  /// 主控在「连接设备 → 协助码」里兑换，兑换即授权，不需要验证码。
  Future<void> _createAssistCode() async {
    try {
      final code = await widget.client.createAssistCode();
      if (!mounted) return;
      if (code.isEmpty) {
        ScaffoldMessenger.of(context)
            .showSnackBar(const SnackBar(content: Text('服务端未返回协助码')));
        return;
      }
      await showDialog<void>(
        context: context,
        builder: (ctx) {
          final scheme = Theme.of(ctx).colorScheme;
          final text = Theme.of(ctx).textTheme;
          return AlertDialog(
            title: const Text('协助码'),
            content: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Container(
                  width: double.infinity,
                  padding: const EdgeInsets.symmetric(vertical: 18),
                  alignment: Alignment.center,
                  decoration: BoxDecoration(
                    color: scheme.primaryContainer,
                    borderRadius: BorderRadius.circular(ErTokens.radiusMd),
                  ),
                  child: Text(
                    code,
                    style: text.headlineMedium?.copyWith(
                      letterSpacing: 8,
                      color: scheme.onPrimaryContainer,
                      fontFeatures: const [FontFeature.tabularFigures()],
                    ),
                  ),
                ),
                const SizedBox(height: 10),
                Text(
                  '10 分钟内有效，只能用一次。把它告诉对方，对方在「连接设备 → 协助码」里兑换。',
                  style: text.bodySmall?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
                ),
              ],
            ),
            actions: [
              TextButton(
                onPressed: () => Navigator.of(ctx).pop(),
                child: const Text('关闭'),
              ),
              FilledButton.icon(
                onPressed: () {
                  Clipboard.setData(ClipboardData(text: code));
                  Navigator.of(ctx).pop();
                  ScaffoldMessenger.of(context)
                      .showSnackBar(SnackBar(content: Text('已复制协助码 ' + code)));
                },
                icon: const Icon(Icons.copy_rounded, size: 16),
                label: const Text('复制'),
              ),
            ],
          );
        },
      );
    } on ApiException catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context)
          .showSnackBar(SnackBar(content: Text('生成失败：' + e.message)));
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context)
          .showSnackBar(SnackBar(content: Text('生成失败：' + e.toString())));
    }
  }

  /// 「被控服务」卡片：开关 + 设备码 + 临时密码（可查看/复制）+ 状态。
  Widget _hostCard(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    final st = _hostStatus;
    final tail = st.sessionId.length > 8
        ? st.sessionId.substring(st.sessionId.length - 8)
        : st.sessionId;
    final supported = HostService.supported;
    final hostGate = CapabilityGate.instance.stateOf('remote_host');
    final statusText = !hostGate.available
        ? "未接入：${hostGate.reason}"
        : !supported
        ? "当前平台不支持被控：浏览器没有原生采集/注入，请用桌面客户端"
        : !_hostOn
        ? "未开启：打开开关后，这台设备才能被远程控制"
        : st.hasSession
        ? "正在被控 · $tail · 已传 ${st.frames} 帧 / 注入 ${st.injected} 次"
        : "已就绪，等待对方连接";

    return Container(
      margin: const EdgeInsets.only(bottom: 16),
      padding: const EdgeInsets.fromLTRB(18, 14, 18, 14),
      decoration: BoxDecoration(
        color: scheme.surfaceContainerLowest,
        borderRadius: BorderRadius.circular(ErTokens.radiusLg),
        border: Border.all(color: scheme.outlineVariant.withValues(alpha: 0.6)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(Icons.computer_rounded, size: 18, color: scheme.primary),
              const SizedBox(width: 8),
              Expanded(child: Text("允许被控（本机共享）", style: text.titleSmall)),
              Switch(
                key: const Key('allow-control-switch'),
                value: hostGate.available && supported && _hostOn,
                onChanged: (!hostGate.available || !supported || _hostBusy)
                    ? null
                    : _toggleHost,
              ),
            ],
          ),
          const SizedBox(height: 4),
          Text(
            statusText,
            style: text.bodySmall?.copyWith(color: scheme.onSurfaceVariant),
          ),
          if (_hostError.isNotEmpty) ...[
            const SizedBox(height: 6),
            Text(
              _hostError,
              style: text.bodySmall?.copyWith(color: scheme.error),
            ),
          ],
          if (_hostOn) ...[
            const SizedBox(height: 10),
            Row(
              children: [
                Text(
                  "临时密码",
                  style: text.labelMedium?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
                ),
                const SizedBox(width: 10),
                Text(
                  _showPw ? (_hostPassword ?? "") : "●" * 8,
                  style: text.titleMedium?.copyWith(letterSpacing: 2),
                ),
                const Spacer(),
                TextButton(
                  onPressed: () => setState(() => _showPw = !_showPw),
                  child: Text(_showPw ? "隐藏" : "显示"),
                ),
                TextButton(
                  onPressed: _hostPassword == null
                      ? null
                      : () async {
                          await Clipboard.setData(
                            ClipboardData(text: _hostPassword!),
                          );
                          if (context.mounted) {
                            ScaffoldMessenger.of(context).showSnackBar(
                              const SnackBar(content: Text("临时密码已复制")),
                            );
                          }
                        },
                  child: const Text("复制"),
                ),
              ],
            ),
            Text(
              "对方在「连接设备」里输入设备码 + 这串密码即可；密码长期有效，可反复使用。",
              style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant),
            ),
          ],
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    final wide = MediaQuery.sizeOf(context).width >= ErTokens.mobileBreakpoint;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (_hostStatus.hasSession)
          LocalControlBar(
            sessionId: _hostStatus.sessionId,
            onDisconnect: _disconnectRemote,
          ),
        PageHeader(
          title: '我的设备',
          subtitle: '本机设备码 ' + widget.ownDevice.prettyCode + ' · 让对方输入这串数字即可连接',
          actions: [
            if (wide)
              SizedBox(
                width: 240,
                child: TextField(
                  controller: _search,
                  onChanged: (_) => setState(() {}),
                  decoration: const InputDecoration(
                    hintText: '搜索设备码或平台',
                    prefixIcon: Icon(Icons.search_rounded, size: 18),
                    isDense: true,
                  ),
                ),
              ),
            OutlinedButton.icon(
              onPressed: _reload,
              icon: const Icon(Icons.refresh_rounded, size: 18),
              label: const Text('刷新'),
            ),
          ],
        ),
        Expanded(
          child: FutureBuilder<List<DeviceInfo>>(
            future: _future,
            builder: (context, snap) {
              final loading = snap.connectionState != ConnectionState.done;
              final all = snap.data ?? const <DeviceInfo>[];
              final query = _search.text.trim();
              final devices = all.where((d) {
                if (_filter == _Filter.online && !d.online) return false;
                if (_filter == _Filter.offline && d.online) return false;
                if (query.isEmpty) return true;
                return d.code.contains(query) ||
                    d.platform.toLowerCase().contains(query.toLowerCase());
              }).toList();
              final online = all.where((d) => d.online).length;

              return ListView(
                padding: EdgeInsets.fromLTRB(
                  wide ? 32 : 16,
                  0,
                  wide ? 32 : 16,
                  32,
                ),
                children: [
                  _OwnDeviceCard(device: widget.ownDevice, onCopy: _copy),
                  const SizedBox(height: 16),
                  // 「允许被控」是本机状态，属于「这台机器能不能被连」，
                  // 跟下面的设备列表不是一类东西，所以放在设备列表**上方**。
                  _hostCard(context),
                  const SizedBox(height: 24),
                  Row(
                    children: [
                      Text('设备', style: text.titleMedium),
                      const SizedBox(width: 8),
                      Container(
                        padding: const EdgeInsets.symmetric(
                          horizontal: 8,
                          vertical: 2,
                        ),
                        decoration: BoxDecoration(
                          color: scheme.surfaceContainer,
                          borderRadius: BorderRadius.circular(
                            ErTokens.radiusPill,
                          ),
                        ),
                        child: Text(
                          online.toString() + ' 在线 / ' + all.length.toString(),
                          style: text.labelSmall?.copyWith(
                            color: scheme.onSurfaceVariant,
                          ),
                        ),
                      ),
                      const Spacer(),
                      _FilterBar(
                        value: _filter,
                        onChanged: (f) => setState(() => _filter = f),
                      ),
                    ],
                  ),
                  const SizedBox(height: 12),
                  if (!wide)
                    Padding(
                      padding: const EdgeInsets.only(bottom: 12),
                      child: TextField(
                        controller: _search,
                        onChanged: (_) => setState(() {}),
                        decoration: const InputDecoration(
                          hintText: '搜索设备码或平台',
                          prefixIcon: Icon(Icons.search_rounded, size: 18),
                          isDense: true,
                        ),
                      ),
                    ),
                  if (loading)
                    const Padding(
                      padding: EdgeInsets.symmetric(vertical: 48),
                      child: Center(child: CircularProgressIndicator()),
                    )
                  else if (snap.hasError)
                    _EmptyHint(
                      icon: Icons.cloud_off_rounded,
                      title: '设备列表加载失败',
                      subtitle: snap.error.toString(),
                      action: TextButton(
                        onPressed: _reload,
                        child: const Text('重试'),
                      ),
                    )
                  else if (devices.isEmpty)
                    const _EmptyHint(
                      icon: Icons.devices_other_rounded,
                      title: '这里还没有设备',
                      subtitle: '在另一台电脑登录后会自动出现在列表里',
                    )
                  else
                    LayoutBuilder(
                      builder: (context, constraints) {
                        final columns = constraints.maxWidth >= 1000
                            ? 3
                            : constraints.maxWidth >= 640
                            ? 2
                            : 1;
                        final cardWidth =
                            (constraints.maxWidth - (columns - 1) * 16) /
                            columns;
                        return Wrap(
                          spacing: 16,
                          runSpacing: 16,
                          children: [
                            for (final d in devices)
                              SizedBox(
                                width: cardWidth,
                                child: _DeviceCard(
                                  device: d,
                                  isOwn:
                                      d.code == widget.ownDevice.code ||
                                      (d.id.isNotEmpty &&
                                          d.id == widget.ownDevice.id),
                                  direct: appPrefs.value.sameAccountDirect,
                                  busy: _directBusyCode == d.code,
                                  onConnect: widget.onConnect,
                                  onDirect: () => _directConnect(d),
                                  onCopy: _copy,
                                ),
                              ),
                          ],
                        );
                      },
                    ),
                  const SizedBox(height: 20),
                  Text('其他连接方式', style: text.titleMedium),
                  const SizedBox(height: 4),
                  Text(
                    '无需在列表里找到设备，直接用设备码连接',
                    style: text.bodySmall?.copyWith(
                      color: scheme.onSurfaceVariant,
                    ),
                  ),
                  const SizedBox(height: 12),
                  LayoutBuilder(
                    builder: (context, constraints) {
                      final columns = constraints.maxWidth >= 720 ? 2 : 1;
                      final cardWidth =
                          (constraints.maxWidth - (columns - 1) * 16) / columns;
                      return Wrap(
                        spacing: 16,
                        runSpacing: 12,
                        children: [
                          SizedBox(
                            width: cardWidth,
                            child: _QuickCard(
                              icon: Icons.pin_rounded,
                              title: '输入设备码',
                              subtitle: '通过 9 位设备码连接另一台电脑',
                              onTap: () => widget.onConnect(''),
                            ),
                          ),
                          SizedBox(
                            width: cardWidth,
                            child: _QuickCard(
                              icon: Icons.key_rounded,
                              title: '生成协助码',
                              subtitle: '给自己这台设备发一个 6 位临时授权码',
                              onTap: _createAssistCode,
                            ),
                          ),
                          SizedBox(
                            width: cardWidth,
                            child: _QuickCard(
                              icon: Icons.sync_alt_rounded,
                              title: '刷新设备状态',
                              subtitle: '重新拉取在线状态与设备列表',
                              onTap: _reload,
                            ),
                          ),
                        ],
                      );
                    },
                  ),
                ],
              );
            },
          ),
        ),
      ],
    );
  }
}

class _OwnDeviceCard extends StatelessWidget {
  const _OwnDeviceCard({required this.device, required this.onCopy});

  final DeviceInfo device;
  final ValueChanged<String> onCopy;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    return Container(
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(ErTokens.radiusLg),
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [
            scheme.primary,
            Color.lerp(scheme.primary, scheme.primaryContainer, 0.45) ??
                scheme.primary,
          ],
        ),
      ),
      child: Row(
        children: [
          Container(
            width: 44,
            height: 44,
            decoration: BoxDecoration(
              color: Colors.white.withValues(alpha: 0.18),
              borderRadius: BorderRadius.circular(12),
            ),
            child: const Icon(
              Icons.computer_rounded,
              color: Colors.white,
              size: 24,
            ),
          ),
          const SizedBox(width: 16),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  '本机设备码',
                  style: text.labelMedium?.copyWith(
                    color: Colors.white.withValues(alpha: 0.85),
                  ),
                ),
                const SizedBox(height: 4),
                Text(
                  device.prettyCode,
                  style: text.headlineSmall?.copyWith(
                    color: Colors.white,
                    letterSpacing: 4,
                    fontFeatures: const [FontFeature.tabularFigures()],
                  ),
                ),
                Text(
                  '让对方在「连接设备」里输入这 9 位数字',
                  style: text.bodySmall?.copyWith(
                    color: Colors.white.withValues(alpha: 0.8),
                  ),
                ),
              ],
            ),
          ),
          IconButton(
            tooltip: '复制设备码',
            onPressed: () => onCopy(device.prettyCode),
            icon: const Icon(Icons.copy_rounded, color: Colors.white),
          ),
        ],
      ),
    );
  }
}

class _DeviceCard extends StatelessWidget {
  const _DeviceCard({
    required this.device,
    required this.isOwn,
    required this.direct,
    required this.busy,
    required this.onConnect,
    required this.onDirect,
    required this.onCopy,
  });

  final DeviceInfo device;
  final bool isOwn;

  /// 设置里的「同账号设备免密直连」是否开启。
  final bool direct;
  final bool busy;
  final ValueChanged<String> onConnect;
  final VoidCallback onDirect;
  final ValueChanged<String> onCopy;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    final online = device.online;
    final isWindows = device.platform.toLowerCase().contains('windows');
    final controlGate = CapabilityGate.instance.stateOf('remote_control');

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
          Row(
            children: [
              Container(
                width: 38,
                height: 38,
                decoration: BoxDecoration(
                  color: scheme.surfaceContainerHigh,
                  borderRadius: BorderRadius.circular(10),
                ),
                child: Icon(
                  isWindows
                      ? Icons.desktop_windows_rounded
                      : Icons.laptop_mac_rounded,
                  size: 20,
                  color: scheme.onSurfaceVariant,
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Flexible(
                          child: Text(
                            isWindows ? 'Windows 设备' : 'macOS 设备',
                            style: text.titleSmall,
                            overflow: TextOverflow.ellipsis,
                          ),
                        ),
                        if (isOwn) ...[
                          const SizedBox(width: 6),
                          Container(
                            padding: const EdgeInsets.symmetric(
                              horizontal: 6,
                              vertical: 1,
                            ),
                            decoration: BoxDecoration(
                              color: scheme.primaryContainer,
                              borderRadius: BorderRadius.circular(
                                ErTokens.radiusPill,
                              ),
                            ),
                            child: Text(
                              '本机',
                              style: text.labelSmall?.copyWith(
                                color: scheme.onPrimaryContainer,
                                fontWeight: FontWeight.w600,
                              ),
                            ),
                          ),
                        ],
                      ],
                    ),
                    Text(
                      device.platform,
                      style: text.labelSmall?.copyWith(
                        color: scheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                ),
              ),
              _StatusChip(online: online),
            ],
          ),
          const SizedBox(height: 14),
          Row(
            children: [
              Expanded(
                child: Text(
                  device.prettyCode,
                  style: text.headlineSmall?.copyWith(
                    letterSpacing: 3,
                    fontFeatures: const [FontFeature.tabularFigures()],
                  ),
                ),
              ),
              IconButton(
                tooltip: '复制设备码',
                visualDensity: VisualDensity.compact,
                onPressed: () => onCopy(device.prettyCode),
                icon: const Icon(Icons.copy_rounded, size: 18),
              ),
            ],
          ),
          const SizedBox(height: 12),
          Row(
            children: [
              Icon(
                online ? Icons.bolt_rounded : Icons.schedule_rounded,
                size: 15,
                color: scheme.onSurfaceVariant,
              ),
              const SizedBox(width: 4),
              Text(
                online ? '在线' : '离线',
                style: text.labelSmall?.copyWith(
                  color: scheme.onSurfaceVariant,
                ),
              ),
              const Spacer(),
              // 本机设备不能"连接自己"：那只会开一个指向本机的会话，纯噪音。
              // 要给别人连自己，看上面的本机设备码。
              if (isOwn)
                Text(
                  '当前设备',
                  style: text.labelSmall?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
                )
              else if (online)
                Tooltip(
                  message: controlGate.available ? '' : controlGate.reason,
                  child: FilledButton.tonal(
                    // 免密直连开启时点一下就发起；关闭时仍走设备码 + 临时密码页。
                    onPressed: (!controlGate.available || busy)
                        ? null
                        : (direct ? onDirect : () => onConnect(device.code)),
                    style: FilledButton.styleFrom(
                      minimumSize: const Size(0, 36),
                      padding: const EdgeInsets.symmetric(horizontal: 16),
                    ),
                    child: busy
                        ? const SizedBox(
                            width: 14,
                            height: 14,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : Text(
                            controlGate.available
                                ? (direct ? '直接连接' : '连接')
                                : '未开放',
                          ),
                  ),
                )
              else
                FilledButton.tonal(
                  onPressed: null,
                  style: FilledButton.styleFrom(
                    minimumSize: const Size(0, 36),
                    padding: const EdgeInsets.symmetric(horizontal: 16),
                  ),
                  child: const Text('离线'),
                ),
            ],
          ),
        ],
      ),
    );
  }
}

class _StatusChip extends StatelessWidget {
  const _StatusChip({required this.online});

  final bool online;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final color = online ? const Color(0xFF2A6239) : scheme.onSurfaceVariant;
    final bg = online ? const Color(0xFFC8F6D0) : scheme.surfaceContainerHigh;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
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
            decoration: BoxDecoration(color: color, shape: BoxShape.circle),
          ),
          const SizedBox(width: 5),
          Text(
            online ? '在线' : '离线',
            style: TextStyle(
              fontSize: 11,
              fontWeight: FontWeight.w600,
              color: color,
            ),
          ),
        ],
      ),
    );
  }
}

class _FilterBar extends StatelessWidget {
  const _FilterBar({required this.value, required this.onChanged});

  final _Filter value;
  final ValueChanged<_Filter> onChanged;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    Widget chip(_Filter f, String label) {
      final on = value == f;
      return InkWell(
        borderRadius: BorderRadius.circular(ErTokens.radiusPill),
        onTap: () => onChanged(f),
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
          decoration: BoxDecoration(
            color: on ? scheme.primaryContainer : Colors.transparent,
            borderRadius: BorderRadius.circular(ErTokens.radiusPill),
          ),
          child: Text(
            label,
            style: text.labelSmall?.copyWith(
              color: on ? scheme.onPrimaryContainer : scheme.onSurfaceVariant,
              fontWeight: on ? FontWeight.w600 : FontWeight.w400,
            ),
          ),
        ),
      );
    }

    return Container(
      padding: const EdgeInsets.all(3),
      decoration: BoxDecoration(
        color: scheme.surfaceContainer,
        borderRadius: BorderRadius.circular(ErTokens.radiusPill),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          chip(_Filter.all, '全部'),
          chip(_Filter.online, '在线'),
          chip(_Filter.offline, '离线'),
        ],
      ),
    );
  }
}

class _QuickCard extends StatelessWidget {
  const _QuickCard({
    required this.icon,
    required this.title,
    required this.subtitle,
    required this.onTap,
  });

  final IconData icon;
  final String title;
  final String subtitle;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    return Material(
      color: scheme.surfaceContainerLow,
      borderRadius: BorderRadius.circular(ErTokens.radiusLg),
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(ErTokens.radiusLg),
        child: Container(
          padding: const EdgeInsets.all(16),
          decoration: BoxDecoration(
            borderRadius: BorderRadius.circular(ErTokens.radiusLg),
            border: Border.all(
              color: scheme.outlineVariant.withValues(alpha: 0.7),
            ),
          ),
          child: Row(
            children: [
              Container(
                width: 38,
                height: 38,
                decoration: BoxDecoration(
                  color: scheme.primaryContainer,
                  borderRadius: BorderRadius.circular(10),
                ),
                child: Icon(icon, size: 20, color: scheme.onPrimaryContainer),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(title, style: text.titleSmall),
                    Text(
                      subtitle,
                      style: text.labelSmall?.copyWith(
                        color: scheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                ),
              ),
              Icon(
                Icons.chevron_right_rounded,
                size: 20,
                color: scheme.onSurfaceVariant,
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _EmptyHint extends StatelessWidget {
  const _EmptyHint({
    required this.icon,
    required this.title,
    required this.subtitle,
    this.action,
  });

  final IconData icon;
  final String title;
  final String subtitle;
  final Widget? action;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 48),
      child: Column(
        children: [
          Icon(icon, size: 40, color: scheme.onSurfaceVariant),
          const SizedBox(height: 12),
          Text(title, style: text.titleSmall),
          const SizedBox(height: 4),
          Text(
            subtitle,
            style: text.bodySmall?.copyWith(color: scheme.onSurfaceVariant),
            textAlign: TextAlign.center,
          ),
          if (action != null) ...[const SizedBox(height: 8), action!],
        ],
      ),
    );
  }
}
