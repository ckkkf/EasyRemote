import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'package:easyremote_client/api.dart';

void main() {
  test('loginEmail completes', () async {
    final client = GatewayClient(baseUrl: 'http://test', client: MockClient((req) async {
      return http.Response('{"code":0,"name":"OK","data":{"token":"t"},"trace_id":"x"}', 200, headers: {'content-type': 'application/json'});
    }));
    print('DBG calling');
    await client.loginEmail('a@b.com', '654321').timeout(const Duration(seconds: 3));
    print('DBG done token=' + (client.token ?? 'null'));
  });
}
