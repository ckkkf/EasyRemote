// ER-905 跨进程 E2E 探针：作为一个**独立主控进程**使用。
//
// 流程：登录 → 绑定本机设备 → 请求连接（设备码 + 临时密码）→ attach → 收帧
//       → 可选主动 close → 退出。
//
// 用法：dart run client/tool/lifecycle_probe.dart <gateway> <relay> <device_code> <password> [--close]
import 'dart:async';
import 'dart:convert';
import 'dart:ffi';
import 'dart:io';

import 'package:ffi/ffi.dart';

typedef _AttachC = Pointer<Utf8> Function(
  Pointer<Utf8>,
  Pointer<Utf8>,
  Pointer<Utf8>,
  Pointer<Utf8>,
  Pointer<Utf8>,
  Pointer<Utf8>,
);
typedef _AttachD = Pointer<Utf8> Function(
  Pointer<Utf8>,
  Pointer<Utf8>,
  Pointer<Utf8>,
  Pointer<Utf8>,
  Pointer<Utf8>,
  Pointer<Utf8>,
);
typedef _InfoC = Pointer<Utf8> Function(Pointer<Utf8>);
typedef _InfoD = Pointer<Utf8> Function(Pointer<Utf8>);
typedef _CloseC = Int32 Function(Pointer<Utf8>);
typedef _CloseD = int Function(Pointer<Utf8>);
typedef _FreeC = Void Function(Pointer<Utf8>);
typedef _FreeD = void Function(Pointer<Utf8>);

Future<Map<String, dynamic>> post(
  String base,
  String path,
  Map<String, dynamic> body, {
  String? token,
  String? deviceToken,
}) async {
  final c = HttpClient();
  final req = await c.postUrl(Uri.parse(base + path));
  req.headers.contentType = ContentType.json;
  if (token != null) req.headers.set('authorization', 'Bearer $token');
  if (deviceToken != null) req.headers.set('x-device-token', deviceToken);
  req.write(jsonEncode(body));
  final resp = await req.close();
  final txt = await resp.transform(utf8.decoder).join();
  c.close();
  final m = jsonDecode(txt) as Map<String, dynamic>;
  return (m['data'] as Map<String, dynamic>?) ?? m;
}

Future<Map<String, dynamic>> get(
  String base,
  String path, {
  String? token,
}) async {
  final c = HttpClient();
  final req = await c.getUrl(Uri.parse(base + path));
  if (token != null) req.headers.set('authorization', 'Bearer $token');
  final resp = await req.close();
  final txt = await resp.transform(utf8.decoder).join();
  c.close();
  final m = jsonDecode(txt) as Map<String, dynamic>;
  return (m['data'] as Map<String, dynamic>?) ?? m;
}

void main(List<String> args) async {
  if (args.length < 4) {
    stderr.writeln(
      'usage: lifecycle_probe <gateway> <relay> <device_code> <password> [--close]',
    );
    exit(2);
  }
  final gateway = args[0];
  final relay = args[1];
  final deviceCode = args[2];
  final password = args[3];
  final doClose = args.contains('--close');

  final login = await post(gateway, '/v1/auth/login', {
    'phone': '13700008202',
    'code': '123456',
  });
  final token = login['token'] as String;
  final bind = await post(gateway, '/v1/devices/bind', {
    'platform': 'probe',
  }, token: token);
  final deviceId = bind['device_id'] as String;
  final deviceToken = bind['device_token'] as String;
  final conn = await post(
    gateway,
    '/v1/connect/request',
    {'device_code': deviceCode, 'captcha': password},
    token: token,
    deviceToken: deviceToken,
  );
  final sid = conn['session_id'] as String;
  stdout.writeln('PROBE_CONNECTED session=$sid');

  final lib = DynamicLibrary.open(
    Platform.environment['ER_CORE_LIB'] ?? 'core/target/debug/liber_ffi.dylib',
  );
  final free = lib.lookupFunction<_FreeC, _FreeD>('er_free_string');
  String take(Pointer<Utf8> p) {
    if (p == nullptr) return '';
    final s = p.toDartString();
    free(p);
    return s;
  }

  // 所有 toNativeUtf8 都必须配对 calloc.free，否则每次探针运行都泄漏字符串。
  String callWithFree(
    List<String> args,
    Pointer<Utf8> Function(List<Pointer<Utf8>>) fn,
  ) {
    final ptrs = args.map((a) => a.toNativeUtf8()).toList();
    try {
      final out = fn(ptrs);
      return take(out);
    } finally {
      for (final p in ptrs) {
        calloc.free(p);
      }
    }
  }

  // 路径 C：只请求、不 attach，等被控端拒绝；2 秒内必须看到 closed/user_rejected。
  if (args.contains('--wait-rejected')) {
    final deadline = DateTime.now().add(const Duration(seconds: 2));
    while (DateTime.now().isBefore(deadline)) {
      final st = await get(
        gateway,
        '/v1/connect/status?session_id=$sid',
        token: token,
      );
      final state = st['state']?.toString() ?? '';
      final reason = st['reason']?.toString() ?? '';
      if (state == 'closed' && reason == 'user_rejected') {
        stdout.writeln('PROBE_REJECTED reason=$reason');
        exit(0);
      }
      await Future<void>.delayed(const Duration(milliseconds: 200));
    }
    stdout.writeln('PROBE_REJECTED timeout');
    exit(1);
  }
  final attach = lib.lookupFunction<_AttachC, _AttachD>('er_ctrl_attach');
  final res = jsonDecode(
    callWithFree([
      gateway,
      relay,
      token,
      deviceId,
      deviceToken,
      sid,
    ], (p) => attach(p[0], p[1], p[2], p[3], p[4], p[5])),
  ) as Map<String, dynamic>;
  if (res['ok'] != true) {
    stdout.writeln('PROBE_ATTACH_FAILED $res');
    exit(1);
  }
  final ffiSid = res['session_id'] as String;
  stdout.writeln('PROBE_ATTACHED $ffiSid');

  final info = lib.lookupFunction<_InfoC, _InfoD>('er_ctrl_info');
  for (var i = 0; i < 20; i++) {
    await Future<void>.delayed(const Duration(milliseconds: 200));
    final st = jsonDecode(
      callWithFree([ffiSid], (p) => info(p[0])),
    ) as Map<String, dynamic>;
    if ((st['frames'] as num?)?.toInt() != null &&
        (st['frames'] as num).toInt() > 0) {
      stdout.writeln('PROBE_FRAMES=${st['frames']}');
      break;
    }
  }

  // 路径 B：主控保持轮询，直到看到对端关闭；2 秒内必须观察到。
  if (args.contains('--watch-peer')) {
    final infoFn = lib.lookupFunction<_InfoC, _InfoD>('er_ctrl_info');
    final deadline = DateTime.now().add(const Duration(seconds: 2));
    while (DateTime.now().isBefore(deadline)) {
      await Future<void>.delayed(const Duration(milliseconds: 200));
      final st = jsonDecode(
        callWithFree([ffiSid], (p) => infoFn(p[0])),
      ) as Map<String, dynamic>;
      final frames = (st['frames'] as num?)?.toInt() ?? 0;
      if (frames > 0) {
        stdout.writeln('PROBE_FRAMES=$frames');
        break;
      }
    }
    final statusDeadline = DateTime.now().add(const Duration(seconds: 2));
    while (DateTime.now().isBefore(statusDeadline)) {
      final st = await get(
        gateway,
        '/v1/connect/status?session_id=$sid',
        token: token,
      );
      if ((st['state']?.toString() ?? '') == 'closed') {
        stdout.writeln(
          'PROBE_PEER_CLOSED reason=${st['reason']?.toString() ?? ''}',
        );
        releaseCtrl(lib, ffiSid);
        exit(0);
      }
      await Future<void>.delayed(const Duration(milliseconds: 200));
    }
    stdout.writeln('PROBE_PEER_CLOSED timeout');
    exit(1);
  }

  if (doClose) {
    final closed = await post(
      gateway,
      '/v1/sessions/close',
      {'session_id': sid, 'reason': 'controller_closed'},
      token: token,
      deviceToken: deviceToken,
    );
    stdout.writeln('PROBE_CLOSED state=${closed['state']?.toString() ?? ''}');
    // A 路径也要释放主控注册表句柄（与 watch-peer 路径一致）
    releaseCtrl(lib, ffiSid);
  }
  exit(0);
}

/// 释放主控注册表句柄（er_ctrl_close）。所有带 attach 的退出路径都必须调用。
void releaseCtrl(DynamicLibrary lib, String ffiSid) {
  final closeFn = lib.lookupFunction<_CloseC, _CloseD>('er_ctrl_close');
  final sidPtr = ffiSid.toNativeUtf8();
  try {
    closeFn(sidPtr);
  } finally {
    calloc.free(sidPtr);
  }
}
