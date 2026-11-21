// STUN Binding 请求/响应的最小编解码。
//
// 只实现 RFC 5389 里诊断需要的一小块：Binding Request + XOR-MAPPED-ADDRESS。
// 独立成纯函数，便于单元测试（不需要真的联网）。
import 'dart:typed_data';

/// STUN magic cookie（RFC 5389 §6）。
const int stunMagicCookie = 0x2112A442;

const int _bindingRequest = 0x0001;
const int _bindingSuccess = 0x0101;
const int _attrMappedAddress = 0x0001;
const int _attrXorMappedAddress = 0x0020;

/// 构造 20 字节 Binding Request。
Uint8List buildStunBindingRequest(List<int> transactionId) {
  if (transactionId.length != 12) {
    throw ArgumentError('transaction id must be 12 bytes');
  }
  final out = Uint8List(20);
  final view = ByteData.sublistView(out);
  view.setUint16(0, _bindingRequest, Endian.big);
  view.setUint16(2, 0, Endian.big); // attribute length
  view.setUint32(4, stunMagicCookie, Endian.big);
  out.setRange(8, 20, transactionId);
  return out;
}

/// STUN 映射地址。
class StunMappedAddress {
  const StunMappedAddress(this.ip, this.port);
  final String ip;
  final int port;

  @override
  String toString() => port == 0 ? ip : '$ip:$port';
}

/// 是否是我们关心的 Binding Success。
bool isStunBindingResponse(Uint8List data) {
  if (data.length < 20) return false;
  final view = ByteData.sublistView(data);
  if (view.getUint16(0, Endian.big) != _bindingSuccess) return false;
  if (view.getUint32(4, Endian.big) != stunMagicCookie) return false;
  final len = view.getUint16(2, Endian.big);
  return 20 + len <= data.length;
}

/// 是否为 Binding Request（自打洞环回时收到的就是自己发出的请求）。
bool isStunBindingRequest(Uint8List data) {
  if (data.length < 20) return false;
  final view = ByteData.sublistView(data);
  if (view.getUint16(0, Endian.big) != _bindingRequest) return false;
  return view.getUint32(4, Endian.big) == stunMagicCookie;
}

/// 从 Binding Success 里取 XOR-MAPPED-ADDRESS（优先）或 MAPPED-ADDRESS。
StunMappedAddress? parseStunMappedAddress(Uint8List data) {
  if (!isStunBindingResponse(data)) return null;
  final view = ByteData.sublistView(data);
  final len = view.getUint16(2, Endian.big);
  var offset = 20;
  final end = 20 + len;
  StunMappedAddress? plain;
  while (offset + 4 <= end) {
    final type = view.getUint16(offset, Endian.big);
    final alen = view.getUint16(offset + 2, Endian.big);
    final body = offset + 4;
    if (body + alen > end) break;
    if (type == _attrXorMappedAddress || type == _attrMappedAddress) {
      final family = data[body + 1];
      if (family == 0x01) {
        if (alen >= 8) {
          var port = view.getUint16(body + 2, Endian.big);
          final ip = Uint8List.fromList(data.sublist(body + 4, body + 8));
          if (type == _attrXorMappedAddress) {
            port ^= (stunMagicCookie >> 16) & 0xFFFF;
            ip[0] ^= (stunMagicCookie >> 24) & 0xFF;
            ip[1] ^= (stunMagicCookie >> 16) & 0xFF;
            ip[2] ^= (stunMagicCookie >> 8) & 0xFF;
            ip[3] ^= stunMagicCookie & 0xFF;
          }
          final mapped = StunMappedAddress(ip.join('.'), port);
          if (type == _attrXorMappedAddress) return mapped;
          plain = mapped;
        }
      }
    }
    // 属性按 4 字节对齐
    offset = body + ((alen + 3) & ~3);
  }
  return plain;
}
