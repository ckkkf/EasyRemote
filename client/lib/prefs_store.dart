// 本机偏好持久化入口：按平台条件导入。
//
//   Web  → prefs_store_web.dart（localStorage: easyremote.prefs.v1）
//   桌面 → prefs_store_io.dart （~/.easyremote/prefs.json）
//   其他 → prefs_store_stub.dart（进程内存）
export 'prefs_store_types.dart';
export 'prefs_store_stub.dart'
    if (dart.library.js_interop) 'prefs_store_web.dart'
    if (dart.library.io) 'prefs_store_io.dart';
