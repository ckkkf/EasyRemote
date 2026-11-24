import 'package:flutter/material.dart';

/// ER-700：被控端本地常驻控制条。
///
/// 只在被控端本地显示，远端无法隐藏/关闭；断开动作直接调用本机服务。
class LocalControlBar extends StatelessWidget {
  const LocalControlBar({
    super.key,
    required this.sessionId,
    required this.onDisconnect,
    this.busy = false,
    this.error = '',
  });

  final String sessionId;
  final VoidCallback onDisconnect;

  /// 断开请求进行中：按钮禁用并显示「断开中…」，避免重复点击被静默吞掉（ER-905）。
  final bool busy;

  /// 上一次断开失败的原因：控制条保留并展示，可重试。
  final String error;

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
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(
                  '正在被远程控制 · 会话 ' + short + '（仅本机可断开）',
                  style: TextStyle(
                    color: scheme.onErrorContainer,
                    fontSize: 13,
                  ),
                ),
                if (error.isNotEmpty)
                  Text(
                    '断开失败：' + error + '（可重试）',
                    key: const Key('local-control-error'),
                    style: TextStyle(
                      color: scheme.onErrorContainer,
                      fontSize: 11,
                    ),
                  ),
              ],
            ),
          ),
          FilledButton.icon(
            key: const Key('local-control-disconnect'),
            onPressed: busy ? null : onDisconnect,
            icon: const Icon(Icons.call_end_rounded, size: 16),
            label: Text(busy ? '断开中…' : '断开远程控制'),
          ),
        ],
      ),
    );
  }
}
