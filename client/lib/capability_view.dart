// Flutter 3.50 起 material.dart 被标 deprecated；全仓迁移到 material_ui 归 ER-001。
// ignore: deprecated_member_use
import 'package:flutter/material.dart';

import 'capabilities.dart';

/// 能力未开放时的统一空态：说清「为什么不可用」和「由哪张卡负责」，
/// 不画可点击的假入口。
class CapabilityUnavailableView extends StatelessWidget {
  const CapabilityUnavailableView({
    super.key,
    required this.title,
    required this.state,
  });

  final String title;
  final CapabilityState state;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    final owner = state.spec?.owner;
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 460),
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Icon(
                Icons.lock_clock_rounded,
                size: 28,
                color: scheme.onSurfaceVariant,
              ),
              const SizedBox(height: 12),
              Text(title, style: text.titleMedium),
              const SizedBox(height: 8),
              Text(
                state.reason,
                style: text.bodySmall?.copyWith(
                  color: scheme.onSurfaceVariant,
                  height: 1.6,
                ),
              ),
              if (owner != null) ...[
                const SizedBox(height: 10),
                Text(
                  '成熟度：${state.spec!.maturity.name} · 收口卡：$owner',
                  style: text.labelSmall?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}
