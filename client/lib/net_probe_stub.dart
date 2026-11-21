// Web 兜底：浏览器沙箱不能创建 UDP socket，STUN 探测在浏览器里无法进行。
//
// 这里不伪造结果：明确告诉用户「这台平台测不了」，同时 TCP 时延/出口地址
// 仍由诊断页自己的 HTTP 请求给出，两者不混在一起。
import 'net_probe_types.dart';

export 'net_probe_types.dart';

class NetProbe {
  static Future<NatProbeResult> probe() async => NatProbeResult.unsupported;
}
