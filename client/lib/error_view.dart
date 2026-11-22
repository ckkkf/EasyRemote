import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'api.dart';
import 'theme.dart';

/// 统一错误视图：消息 + code/name + trace_id（可复制） + 按 retryable 决定的重试。
///
/// 为什么单独抽出来：错误详情散落在各页面，每个页面各写一遍就会漏掉 trace_id/重试语义。
class ApiErrorView extends StatelessWidget {
  const ApiErrorView({
    super.key,
    required this.error,
    this.onRetry,
    this.compact = false,
  });

  final ApiException error;
  final VoidCallback? onRetry;
  final bool compact;

  bool get _canRetry => error.retryable && onRetry != null;

  Future<void> _copyTrace(BuildContext context) async {
    final trace = error.traceId;
    if (trace.isEmpty) return;
    await Clipboard.setData(ClipboardData(text: trace));
    if (!context.mounted) return;
    ScaffoldMessenger.maybeOf(context)
        ?.showSnackBar(SnackBar(content: Text('已复制 trace_id：$trace')));
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    return Container(
      key: const Key('api-error-view'),
      padding: EdgeInsets.all(compact ? 8 : 12),
      decoration: BoxDecoration(
        color: scheme.errorContainer.withValues(alpha: 0.5),
        borderRadius: BorderRadius.circular(ErTokens.radiusMd),
        border: Border.all(color: scheme.error.withValues(alpha: 0.4)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            error.message,
            style: text.bodySmall?.copyWith(color: scheme.onErrorContainer),
          ),
          const SizedBox(height: 6),
          Row(
            children: [
              Flexible(
                child: Text(
                  '${error.displayName}${error.traceId.isEmpty ? '' : ' · trace ${error.traceId}'}',
                  style: text.labelSmall?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
                  overflow: TextOverflow.ellipsis,
                ),
              ),
              if (error.traceId.isNotEmpty)
                IconButton(
                  key: const Key('copy-trace-id'),
                  tooltip: '复制 trace_id',
                  iconSize: 16,
                  visualDensity: VisualDensity.compact,
                  onPressed: () => _copyTrace(context),
                  icon: const Icon(Icons.copy_rounded),
                ),
            ],
          ),
          if (_canRetry)
            Align(
              alignment: Alignment.centerRight,
              child: TextButton(
                key: const Key('retry-error'),
                onPressed: onRetry,
                child: const Text('重试'),
              ),
            ),
        ],
      ),
    );
  }
}
