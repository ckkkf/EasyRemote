// STUN 编解码回归：诊断页的 NAT 结论完全依赖这里解析出的映射地址。
import 'dart:typed_data';

import 'package:easyremote_client/net_probe_stun.dart';
import 'package:flutter_test/flutter_test.dart';

Uint8List _response({required String ip, required int port, bool xor = true}) {
  final out = Uint8List(20 + 12);
  final view = ByteData.sublistView(out);
  view.setUint16(0, 0x0101, Endian.big);
  view.setUint16(2, 12, Endian.big);
  view.setUint32(4, stunMagicCookie, Endian.big);
  for (var i = 0; i < 12; i++) {
    out[8 + i] = i;
  }
  final attr = 20;
  view.setUint16(attr, xor ? 0x0020 : 0x0001, Endian.big);
  view.setUint16(attr + 2, 8, Endian.big);
  out[attr + 4] = 0;
  out[attr + 5] = 0x01; // IPv4
  var wirePort = port;
  final parts = ip.split('.').map(int.parse).toList();
  final octets = List<int>.from(parts);
  if (xor) {
    wirePort = port ^ 0x2112;
    octets[0] ^= (stunMagicCookie >> 24) & 0xFF;
    octets[1] ^= (stunMagicCookie >> 16) & 0xFF;
    octets[2] ^= (stunMagicCookie >> 8) & 0xFF;
    octets[3] ^= stunMagicCookie & 0xFF;
  }
  view.setUint16(attr + 6, wirePort, Endian.big);
  out.setRange(attr + 8, attr + 12, octets);
  return out;
}

void main() {
  test('Binding Request 头部符合 RFC 5389', () {
    final req = buildStunBindingRequest(List<int>.filled(12, 7));
    final view = ByteData.sublistView(req);
    expect(req.length, 20);
    expect(view.getUint16(0, Endian.big), 0x0001);
    expect(view.getUint16(2, Endian.big), 0);
    expect(view.getUint32(4, Endian.big), stunMagicCookie);
    expect(req.sublist(8, 20), List<int>.filled(12, 7));
  });

  test('解析 XOR-MAPPED-ADDRESS', () {
    final data = _response(ip: '203.0.113.7', port: 54321);
    expect(isStunBindingResponse(data), isTrue);
    final mapped = parseStunMappedAddress(data);
    expect(mapped, isNotNull);
    expect(mapped!.ip, '203.0.113.7');
    expect(mapped.port, 54321);
  });

  test('没有 XOR 属性时回退 MAPPED-ADDRESS', () {
    final data = _response(ip: '198.51.100.9', port: 6000, xor: false);
    final mapped = parseStunMappedAddress(data);
    expect(mapped, isNotNull);
    expect(mapped!.ip, '198.51.100.9');
    expect(mapped.port, 6000);
  });

  test('非 Binding Success 不误判', () {
    final data = _response(ip: '203.0.113.7', port: 1);
    data[1] = 0x11; // 0x0111 error response
    expect(isStunBindingResponse(data), isFalse);
    expect(parseStunMappedAddress(data), isNull);
  });

  test('环回时收到的 Binding Request 可识别（hairpin 自检依据）', () {
    final req = buildStunBindingRequest(List<int>.filled(12, 3));
    expect(isStunBindingRequest(req), isTrue);
    expect(isStunBindingResponse(req), isFalse);
  });
}
