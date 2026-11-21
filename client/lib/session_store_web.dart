// Web 实现：浏览器 localStorage。
//
// 刷新、关标签页、重开都还在；同源策略保证别的站点读不到。
// 注意：这是「本机浏览器」的存储，换浏览器/无痕窗口等于没登录过——这是预期行为。
import 'dart:convert';
import 'dart:js_interop';
import 'dart:js_interop_unsafe';

import 'session_store_types.dart';

const String _key = 'easyremote.session.v1';

@JS('localStorage')
external JSObject get _storage;

// localStorage 在隐私模式/被禁用时会直接抛（SecurityError/QuotaExceededError）。
// 持久化只是增强项：读写失败就当没登录过，绝不能让启动流程崩掉。
String? _readRaw() {
  try {
    final v = _storage.callMethod<JSAny?>('getItem'.toJS, _key.toJS);
    if (v == null || v.isUndefinedOrNull) return null;
    return (v as JSString).toDart;
  } catch (_) {
    return null;
  }
}

void _writeRaw(String value) {
  try {
    _storage.callMethod<JSAny?>('setItem'.toJS, _key.toJS, value.toJS);
  } catch (_) {}
}

void _dropRaw() {
  try {
    _storage.callMethod<JSAny?>('removeItem'.toJS, _key.toJS);
  } catch (_) {}
}

Future<StoredSession?> loadSession() async {
  final raw = _readRaw();
  if (raw == null || raw.isEmpty) return null;
  try {
    return StoredSession.fromJson(jsonDecode(raw) as Map<String, dynamic>);
  } catch (_) {
    // 版本升级/手改坏了就当没登录过，别让页面卡在启动态。
    _dropRaw();
    return null;
  }
}

Future<void> saveSession(StoredSession session) async =>
    _writeRaw(jsonEncode(session.toJson()));

Future<void> clearSession() async => _dropRaw();
