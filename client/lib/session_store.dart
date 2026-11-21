// 会话持久化入口：按平台条件导入。
//
//   Web  → session_store_web.dart（localStorage）
//   桌面 → session_store_io.dart （~/.easyremote/session.json）
//   其他 → session_store_stub.dart（进程内存）
export 'session_store_types.dart';
export 'session_store_stub.dart'
    if (dart.library.js_interop) 'session_store_web.dart'
    if (dart.library.io) 'session_store_io.dart';
