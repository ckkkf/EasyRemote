import 'package:flutter/material.dart';

/// ER-700：被控端本地常驻控制条。
///
/// 只在被控端本地显示，远端无法隐藏/关闭；断开动作直接调用本机服务。
class LocalControlBar extends StatelessWidget {
  const LocalControlBar({
    super.key,
    required this.sessionId,
    required this.onDisconnect,
  });

  final String sessionId;
  final VoidCallback onDisconnect;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final short = sessionId.length > 8
        ? sessionId.substring(sessionId.length - 8)
        : sessionId;
    return Container(
      key: const Key('local-control-bar'),
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
      color: scheme.errorContainer,
      child: Row(
        children: [
          Icon(Icons.shield_rounded, size: 18, color: scheme.onErrorContainer),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              '正在被远程控制 · 会话 ' + short + '（仅本机可断开）',
              style: TextStyle(color: scheme.onErrorContainer, fontSize: 13),
            ),
          ),
          FilledButton.icon(
            key: const Key('local-control-disconnect'),
            onPressed: onDisconnect,
            icon: const Icon(Icons.call_end_rounded, size: 16),
            label: const Text('断开远程控制'),
          ),
        ],
      ),
    );
  }
}
