// 网络探测入口：按平台条件导入。
//
//   IO（桌面）→ net_probe_io.dart（真实 UDP STUN / NAT / hairpin）
//   Web       → net_probe_stub.dart（如实报告平台不支持，不伪造数据）
export 'net_probe_stub.dart' if (dart.library.io) 'net_probe_io.dart';
