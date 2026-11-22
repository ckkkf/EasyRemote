import 'package:easyremote_client/api.dart';
import 'package:easyremote_client/error_view.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

Widget wrap(ApiErrorView view) => MaterialApp(
  home: Scaffold(body: Center(child: view)),
);

void main() {
  testWidgets('TestErrorViewShowsTraceAndRetryOnlyWhenRetryable', (
    tester,
  ) async {
    final copied = <String>[];
    tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
      SystemChannels.platform,
      (call) async {
        if (call.method == 'Clipboard.setData') {
          copied.add((call.arguments as Map)['text'] as String);
        }
        return null;
      },
    );
    addTearDown(
      () => tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        null,
      ),
    );

    var retries = 0;
    await tester.pumpWidget(
      wrap(
        ApiErrorView(
          error: ApiException(
            'AUTH_TOO_MANY_ATTEMPTS',
            '登录尝试过多',
            retryable: true,
            traceId: 'trace-123',
            statusCode: 429,
          ),
          onRetry: () => retries++,
        ),
      ),
    );

    expect(find.text('登录尝试过多'), findsOneWidget);
    expect(find.textContaining('trace-123'), findsOneWidget);
    expect(find.byKey(const Key('retry-error')), findsOneWidget);

    await tester.tap(find.byKey(const Key('copy-trace-id')));
    await tester.pump();
    expect(copied, contains('trace-123'));

    await tester.tap(find.byKey(const Key('retry-error')));
    expect(retries, 1);
  });

  testWidgets('不可重试错误不显示重试按钮', (tester) async {
    await tester.pumpWidget(
      wrap(
        const ApiErrorView(
          error: ApiException('AUTH_INVALID_CREDENTIAL', '验证码错误'),
        ),
      ),
    );
    expect(find.byKey(const Key('retry-error')), findsNothing);
    expect(find.byKey(const Key('copy-trace-id')), findsNothing);
  });
}
