// 被控端服务入口：按平台条件导入。
//
//   IO（桌面）→ host_service_io.dart（真实 FFI）
//   Web       → host_service_stub.dart（浏览器里没有原生采集/注入，直接报「需要桌面端」）
export 'host_service_stub.dart' if (dart.library.io) 'host_service_io.dart';
