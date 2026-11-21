// 进程级 NAT 探测缓存。
//
// 会话页的连接信息、质量抽屉、诊断页都要展示同一个 NAT 结论；
// 每处都发一轮 STUN 既慢又可能得到不一致的结果。第一次探测后缓存，
// 用户主动点「开始诊断」时用 force 重新测；并发的首次请求共用同一个 Future。
import 'net_probe.dart';

NatProbeResult? _cached;
Future<NatProbeResult>? _inFlight;

Future<NatProbeResult> probeNatCached({bool force = false}) async {
  if (!force) {
    final hit = _cached;
    if (hit != null) return hit;
    final running = _inFlight;
    if (running != null) return running;
  }
  final future = NetProbe.probe();
  _inFlight = future;
  try {
    final result = await future;
    _cached = result;
    return result;
  } finally {
    if (identical(_inFlight, future)) _inFlight = null;
  }
}

NatProbeResult? get cachedNatProbe => _cached;
