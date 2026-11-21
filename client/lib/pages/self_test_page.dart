import 'dart:isolate';

import 'package:flutter/material.dart';

import '../native.dart' if (dart.library.js_interop) '../native_stub.dart';
import '../theme.dart';

/// 顶层 serve 任务：FFI 阻塞调用必须离开 UI isolate。
Map<String, dynamic> _serveInIsolate(Map<String, String> args) {
  return ErHostLink.load().serve(
        hostId: args['hostId']!,
        signalBase: args['signalBase']!,
        frames: 5,
        timeoutSecs: 30,
      );
}

/// 会话自检：完整走「被控端等信令 → 主控用设备码+验证码连接」的真实流程。
class SelfTestPage extends StatefulWidget {
  const SelfTestPage({super.key});

  @override
  State<SelfTestPage> createState() => _SelfTestPageState();
}

class _SelfTestPageState extends State<SelfTestPage> {
  final _gateway = TextEditingController(text: 'http://127.0.0.1:18099');
  final _relay = TextEditingController(text: '127.0.0.1:19121');

  String? _hostId;
  String? _deviceCode;
  String? _captcha;
  Map<String, dynamic>? _served;
  Map<String, dynamic>? _connected;
  String? _error;
  bool _busy = false;

  @override
  void dispose() {
    _gateway.dispose();
    _relay.dispose();
    if (_hostId != null) {
      ErHostLink.load().release(_hostId!);
    }
    super.dispose();
  }

  Future<void> _run(Future<void> Function() action) async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await action();
    } catch (e) {
      setState(() => _error = e.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _prepare() async {
    await _run(() async {
      final r = ErHostLink.load().prepare(
        gatewayUrl: _gateway.text.trim(),
        relayAddr: _relay.text.trim(),
        phone: '13700050001',
      );
      if (r['ok'] != true) {
        setState(() => _error = r['error']?.toString() ?? 'prepare failed');
        return;
      }
      setState(() {
        _hostId = r['host_id'] as String?;
        _deviceCode = r['device_code'] as String?;
        _captcha = r['captcha'] as String?;
        _served = null;
        _connected = null;
      });
    });
  }

  Future<void> _serve() async {
    final hostId = _hostId;
    if (hostId == null) return;
    await _run(() async {
      final r = await Isolate.run(() => _serveInIsolate({
            'hostId': hostId,
            'signalBase': _gateway.text.trim(),
          }));
      setState(() => _served = r);
    });
  }

  Future<void> _connect() async {
    final code = _deviceCode;
    final captcha = _captcha;
    if (code == null || captcha == null) return;
    await _run(() async {
      final r = ErHostLink.load().connect(
        gatewayUrl: _gateway.text.trim(),
        relayAddr: _relay.text.trim(),
        phone: '13700050002',
        deviceCode: code,
        captcha: captcha,
        frames: 5,
      );
      setState(() => _connected = r);
    });
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return SingleChildScrollView(
      padding: const EdgeInsets.all(ErTokens.gapLg),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('会话自检', style: Theme.of(context).textTheme.headlineSmall),
          const SizedBox(height: 8),
          Text(
            '被控端经信令等待连接，主控用设备码 + 验证码加入。走的是真实服务与真实采集。',
            style: TextStyle(color: scheme.onSurfaceVariant),
          ),
          const SizedBox(height: ErTokens.gapLg),
          TextField(
            key: const Key('gateway-field'),
            controller: _gateway,
            decoration: const InputDecoration(labelText: '网关/信令地址', border: OutlineInputBorder()),
          ),
          const SizedBox(height: ErTokens.gap),
          TextField(
            key: const Key('relay-field'),
            controller: _relay,
            decoration: const InputDecoration(labelText: '中继地址', border: OutlineInputBorder()),
          ),
          const SizedBox(height: ErTokens.gapLg),
          Wrap(
            spacing: 12,
            runSpacing: 12,
            children: [
              FilledButton(
                key: const Key('prepare-host'),
                onPressed: _busy ? null : _prepare,
                child: const Text('准备被控端'),
              ),
              FilledButton.tonal(
                key: const Key('serve-host'),
                onPressed: _busy || _hostId == null ? null : _serve,
                child: const Text('等待连接'),
              ),
              FilledButton.tonal(
                key: const Key('connect-device'),
                onPressed: _busy || _deviceCode == null ? null : _connect,
                child: const Text('用设备码连接'),
              ),
            ],
          ),
          if (_deviceCode != null) ...[
            const SizedBox(height: ErTokens.gapLg),
            Card(
              child: ListTile(
                leading: const Icon(Icons.pin),
                title: Text(
                  _deviceCode!,
                  key: const Key('device-code'),
                  style: const TextStyle(fontSize: 24, letterSpacing: 4),
                ),
                subtitle: Text('设备码　验证码 ${_captcha ?? '-'}'),
              ),
            ),
          ],
          if (_error != null) ...[
            const SizedBox(height: ErTokens.gap),
            Text(_error!, key: const Key('self-test-error'), style: TextStyle(color: scheme.error)),
          ],
          if (_connected != null) ...[
            const SizedBox(height: ErTokens.gapLg),
            Card(
              key: const Key('connect-result'),
              child: ListTile(
                leading: const Icon(Icons.link),
                title: Text(_connected!['ok'] == true ? '主控已加入会话' : '连接失败'),
                subtitle: Text(_connected!.toString()),
              ),
            ),
          ],
          if (_served != null) ...[
            const SizedBox(height: ErTokens.gapLg),
            Card(
              key: const Key('serve-result'),
              child: Padding(
                padding: const EdgeInsets.all(ErTokens.gap),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text('被控端结果', style: Theme.of(context).textTheme.titleMedium),
                    const SizedBox(height: 8),
                    Text('收到帧数：${_served!['frames']}'),
                    Text('画面尺寸：${_served!['width']}×${_served!['height']}'),
                  ],
                ),
              ),
            ),
          ],
        ],
      ),
    );
  }
}
