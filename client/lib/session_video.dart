// 会话画面入口：桌面走 FFI，Web 走 stub（浏览器没有原生解码，保持占位说明）。
export 'session_video_stub.dart' if (dart.library.io) 'session_video_io.dart';
