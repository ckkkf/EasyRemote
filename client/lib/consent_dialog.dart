import 'package:flutter/material.dart';

import 'consent_controller.dart';

/// C18 授权弹窗：控制者信息 + 30 秒倒计时 + 接受/拒绝。
///
/// 只是 ConsentController 的渲染层：倒计时和超时由 controller 驱动，
/// 弹窗自身不持有会话真相、也不自行迁移状态。
class ConsentDialog extends StatelessWidget {
  const ConsentDialog({super.key, required this.controller});

  final ConsentController controller;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    return AnimatedBuilder(
      animation: controller,
      builder: (context, _) {
        final req = controller.pending;
        if (req == null) {
          return const SizedBox.shrink();
        }
        return AlertDialog(
          key: const Key('consent-dialog'),
          title: const Text('远程控制请求'),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('控制者：${req.controllerLabel}', style: text.bodyMedium),
              const SizedBox(height: 8),
              Text(
                '剩余 ${controller.remainingSeconds} 秒；超时将自动拒绝。',
                key: const Key('consent-countdown'),
                style: text.bodySmall?.copyWith(color: scheme.onSurfaceVariant),
              ),
              const SizedBox(height: 12),
              Text(
                '只有在同意后对方才能看到本机画面并操作。这些设置只能在本机修改。',
                style: text.labelSmall?.copyWith(
                  color: scheme.onSurfaceVariant,
                ),
              ),
            ],
          ),
          actions: [
            TextButton(
              key: const Key('consent-reject'),
              onPressed: () => controller.reject(),
              child: const Text('拒绝'),
            ),
            FilledButton(
              key: const Key('consent-accept'),
              onPressed: () => controller.accept(),
              child: const Text('接受'),
            ),
          ],
        );
      },
    );
  }
}
