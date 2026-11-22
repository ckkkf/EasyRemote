import 'dart:convert';

import 'package:easyremote_client/api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

GatewayClient clientReturning(Map<String, dynamic> body, int status) {
  return GatewayClient(
    baseUrl: 'http://test',
    client: MockClient(
      (req) async => http.Response(
        jsonEncode(body),
        status,
        headers: {'content-type': 'application/json'},
      ),
    ),
  );
}

void main() {
  test('TestApiExceptionKeepsEnvelopeFields', () async {
    final client = clientReturning({
      'code': 100004,
      'name': 'AUTH_TOO_MANY_ATTEMPTS',
      'message': '登录尝试过多',
      'retryable': true,
      'trace_id': 'trace-abc',
    }, 429);
    try {
      await client.sendSms('13800000000');
      fail('expected ApiException');
    } on ApiException catch (e) {
      expect(e.code, 'AUTH_TOO_MANY_ATTEMPTS');
      expect(e.name, 'AUTH_TOO_MANY_ATTEMPTS');
      expect(e.message, '登录尝试过多');
      expect(e.retryable, isTrue);
      expect(e.traceId, 'trace-abc');
      expect(e.statusCode, 429);
    }
  });

  test('TestNonMapSuccessDataIsNotDropped', () async {
    final client = clientReturning({
      'code': 0,
      'name': 'OK',
      'data': [1, 2, 3],
      'trace_id': 'trace-list',
    }, 200);
    try {
      await client.fetchConfig();
      fail('非对象 data 必须报错，而不是静默返回空 Map');
    } on ApiException catch (e) {
      expect(e.code, 'SRV_INTERNAL');
      expect(e.traceId, 'trace-list');
      expect(e.message, contains('非对象 data'));
    }
  });

  test('TestMapSuccessDataStillParses', () async {
    final client = clientReturning({
      'code': 0,
      'name': 'OK',
      'data': {
        'config': {
          'session': {'max_minutes': 720},
          'file': {'max_bytes': 1024},
          'diag': {'auto_refresh_secs': 30},
          'client': {'force_update': false},
          'capabilities': <String, bool>{},
        },
      },
      'trace_id': 't',
    }, 200);
    final cfg = await client.fetchConfig();
    expect(cfg.sessionMaxMinutes, 720);
    expect(cfg.fileMaxBytes, 1024);
  });
}
