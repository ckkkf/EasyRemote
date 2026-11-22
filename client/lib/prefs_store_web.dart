// Web 实现：浏览器 localStorage。
//
// 刷新、关标签页、重开都还在；同源策略保证别的站点读不到。
import 'dart:convert';
import 'dart:js_interop';
import 'dart:js_interop_unsafe';

import 'prefs_store_types.dart';

const String _key = 'easyremote.prefs.v1';

@JS('localStorage')
external JSObject get _storage;

// 隐私模式/被禁用时 localStorage 会抛：持久化只是增强项，读写失败就当默认值。
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

Future<Prefs> loadPrefs() async {
  final raw = _readRaw();
  if (raw == null || raw.isEmpty) return const Prefs();
  try {
    return Prefs.fromJson(jsonDecode(raw) as Map<String, dynamic>);
  } catch (_) {
    return const Prefs();
  }
}

Future<void> savePrefs(Prefs prefs) async =>
    _writeRaw(jsonEncode(prefs.toJson()));
