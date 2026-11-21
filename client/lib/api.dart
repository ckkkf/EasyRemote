import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter/foundation.dart' show ValueNotifier;
import 'package:http/http.dart' as http;

import 'models.dart';
import 'session_store.dart';

/// 网关客户端。错误统一抛 [ApiException]，由 UI 决定呈现方式。
class ApiException implements Exception {
  ApiException(this.code, this.message);
  final String code;
  final String message;
  @override
  String toString() => message;
}

class GatewayClient {
  GatewayClient({required this.baseUrl, http.Client? client})
    : _client = client ?? http.Client();

  final String baseUrl;
  final http.Client _client;
  String? _token;
  String? _deviceToken;
  // 本机设备身份：刷新后要靠它把 HomeShell 重新拼起来（服务端绑定是幂等的，不必重新 bind）。
  String? _deviceId;
  String? _deviceCode;
  String? _devicePlatform;

  String? get token => _token;
  String? get deviceToken => _deviceToken;
  String? get deviceId => _deviceId;
  String? get deviceCode => _deviceCode;

  /// 登录态失效计数器。
  ///
  /// 任何一次请求拿到 AUTH_TOKEN_EXPIRED / AUTH_TOKEN_REVOKED，就在这里 +1；
  /// App 根节点监听它并一次性清空导航栈回登录页。之前只有启动时校验，
  /// 页面内令牌过期后每个接口各自报错，用户永远停在「加载失败」上。
  final ValueNotifier<int> authExpired = ValueNotifier<int>(0);

  /// 同一个令牌只通知一次，避免并发请求把登录页叠很多层。
  bool _authExpiredNotified = false;

  /// 启动恢复期间不要触发「跳登录页」通知：
  /// 此时根节点还没挂好主界面，restoreSession 自己会决定回登录页还是留在本地身份。
  bool _suppressAuthNotify = false;

  Map<String, String> _headers({bool device = false}) {
    final h = {'content-type': 'application/json'};
    if (_token != null) h['authorization'] = 'Bearer $_token';
    if (device && _deviceToken != null) h['x-device-token'] = _deviceToken!;
    return h;
  }

  Future<Map<String, dynamic>> _post(
    String path,
    Map<String, dynamic> body, {
    bool device = false,
  }) async {
    final res = await _client.post(
      Uri.parse('$baseUrl$path'),
      headers: _headers(device: device),
      body: jsonEncode(body),
    );
    return _decode(res);
  }

  Future<Map<String, dynamic>> _get(String path, {bool device = false}) async {
    final res = await _client.get(
      Uri.parse('$baseUrl$path'),
      headers: _headers(device: device),
    );
    return _decode(res);
  }

  Map<String, dynamic> _decode(http.Response res) {
    final raw = res.body.trim();
    Map<String, dynamic> body;
    if (raw.isEmpty) {
      body = <String, dynamic>{};
    } else {
      final dynamic decoded;
      try {
        decoded = jsonDecode(raw);
      } on FormatException {
        // 反向代理/网关出错时经常直接回纯文本或 HTML（"bad gateway"、502 页面），
        // 原样抛 FormatException 会让界面显示一串 SyntaxError，用户完全不知道该干什么。
        throw ApiException(
          'SRV_INTERNAL',
          _nonJsonMessage(res.statusCode, raw),
        );
      }
      if (decoded is! Map<String, dynamic>) {
        throw ApiException(
          'SRV_INTERNAL',
          _nonJsonMessage(res.statusCode, raw),
        );
      }
      body = decoded;
    }
    if (res.statusCode >= 400) {
      // 服务端错误信封是扁平的 {code,name,message,retryable,trace_id}（见 docs/spec/error-codes.md §3），
      // 同时兼容旧式 {error:{...}} 包装，避免解析漂移把真实错误码吞成 UNKNOWN。
      final err = (body['error'] as Map<String, dynamic>?) ?? body;
      final code =
          err['name'] as String? ?? err['code']?.toString() ?? 'UNKNOWN';
      if (_isTokenDead(code)) _notifyAuthExpired();
      throw ApiException(
        code,
        err['message'] as String? ?? '请求失败（HTTP ${res.statusCode}）',
      );
    }
    // 成功信封：{code:0, name:"OK", data, trace_id}，业务体在 data 里。
    final data = body['data'];
    return data is Map<String, dynamic> ? data : <String, dynamic>{};
  }

  /// 非 JSON 响应（网关/反代错误页）转成人话。
  String _nonJsonMessage(int status, String raw) {
    final sample = _bodySample(raw);
    if (status == 502 || status == 503 || status == 504) {
      return '网关暂时不可用（HTTP $status'
          '${sample.isEmpty ? '' : ' · $sample'}）：服务端可能未启动，或反向代理连不上后端。';
    }
    return '网关返回了非 JSON 响应（HTTP $status'
        '${sample.isEmpty ? '' : ' · $sample'}）：请确认服务端与反向代理正常。';
  }

  String _bodySample(String raw) {
    final text = raw.trim();
    if (text.isEmpty) return '';
    if (text.startsWith('<')) return 'HTML 页面';
    final flat = text.replaceAll(RegExp(r'\s+'), ' ');
    return flat.length > 60 ? '${flat.substring(0, 60)}…' : flat;
  }

  Future<void> sendSms(String phone) async {
    await _post('/v1/auth/sms', {'phone': phone});
  }

  Future<void> login(String phone, String code) async {
    final body = await _post('/v1/auth/login', {'phone': phone, 'code': code});
    _token = body['token'] as String?;
    _authExpiredNotified = false;
    await _persist();
  }

  Future<DeviceInfo> bindDevice(String platform) async {
    final body = await _post('/v1/devices/bind', {'platform': platform});
    _deviceToken = body['device_token'] as String?;
    final device = DeviceInfo(
      id: body['device_id'] as String? ?? '',
      code: (body['raw_code'] as String? ?? '').replaceAll(' ', ''),
      platform: platform,
      online: true,
    );
    _deviceId = device.id;
    _deviceCode = device.code;
    _devicePlatform = device.platform;
    await _persist();
    return device;
  }

  Future<List<DeviceInfo>> listDevices() async {
    final body = await _get('/v1/devices');
    final list = body['devices'] as List<dynamic>? ?? const [];
    return list
        .map((e) => DeviceInfo.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  // ---------- 登录态持久化 ----------

  /// 用本地会话恢复登录态。
  ///
  /// 刷新页面/重启客户端不该再走一遍登录：令牌存在 localStorage（Web）或
  /// ~/.easyremote/session.json（桌面），这里读回来并**真打一次 /v1/devices** 校验：
  /// 令牌过期/被吊销 → 清干净回登录页；服务端没起/断网 → 先用本地设备身份进主界面。
  Future<DeviceInfo?> restoreSession() async {
    final stored = await loadSession();
    if (stored == null) return null;

    _suppressAuthNotify = true;
    _token = stored.token;
    _deviceToken = stored.deviceToken.isEmpty ? null : stored.deviceToken;
    _deviceId = stored.deviceId;
    _deviceCode = stored.deviceCode;
    _devicePlatform = stored.devicePlatform;

    try {
      final devices = await listDevices();
      DeviceInfo? self;
      for (final d in devices) {
        if (d.id == stored.deviceId) {
          self = d;
          break;
        }
      }
      if (self == null && devices.isNotEmpty) self = devices.first;
      if (self == null) return _localDevice(stored);
      _deviceId = self.id;
      _deviceCode = self.code;
      _devicePlatform = self.platform;
      await _persist();
      return self;
    } on ApiException catch (e) {
      if (_isAuthError(e.code)) {
        await logout();
        return null;
      }
      return _localDevice(stored);
    } catch (_) {
      // 断网/服务端未起：不把用户踢回登录页，先按本地身份进主界面，页面自己会报错。
      return _localDevice(stored);
    } finally {
      _suppressAuthNotify = false;
    }
  }

  /// 退出登录：清内存令牌 + 清本地快照。
  Future<void> logout() async {
    _token = null;
    _deviceToken = null;
    _deviceId = null;
    _deviceCode = null;
    _devicePlatform = null;
    await clearSession();
  }

  /// 令牌不可用：清会话并通知根节点跳登录页。
  void _notifyAuthExpired() {
    if (_suppressAuthNotify || _authExpiredNotified || _token == null) return;
    _authExpiredNotified = true;
    unawaited(logout());
    authExpired.value++;
  }

  DeviceInfo _localDevice(StoredSession stored) => DeviceInfo(
    id: stored.deviceId,
    code: stored.deviceCode,
    platform: stored.devicePlatform.isEmpty ? 'unknown' : stored.devicePlatform,
    online: false,
  );

  /// 只有「令牌本身不可用」才需要清会话；其它错误（限流、封禁）让页面自己提示。
  bool _isAuthError(String code) =>
      _isTokenDead(code) ||
      code == 'AUTH_DEVICE_NOT_BOUND' ||
      code == 'AUTH_INVALID_CREDENTIAL';

  /// 登录态真的没了的码：令牌过期/被吊销。设备未绑定另走 ensureDevice 自愈。
  bool _isTokenDead(String code) =>
      code == 'AUTH_TOKEN_EXPIRED' || code == 'AUTH_TOKEN_REVOKED';

  /// 把当前令牌与设备身份写进本地快照；没有令牌就什么也不写。
  Future<void> _persist() async {
    final token = _token;
    if (token == null || token.isEmpty) return;
    await saveSession(
      StoredSession(
        token: token,
        deviceToken: _deviceToken ?? '',
        deviceId: _deviceId ?? '',
        deviceCode: _deviceCode ?? '',
        devicePlatform: _devicePlatform ?? '',
      ),
    );
  }

  /// 发起连接。服务端要求主控设备凭证（X-Device-Token），
  /// 首次连接未建立信任时需要对方屏幕上的验证码。
  /// [direct] 只在客户端设置「同账号设备免密直连」打开时为 true。
  /// 服务端仅对「目标设备与主控同一账号」放行；跨账号仍要求临时密码。
  Future<String> requestConnect(
    String deviceCode, {
    String? captcha,
    bool direct = false,
  }) async {
    final body = await _post('/v1/connect/request', {
      'device_code': deviceCode,
      if (captcha != null && captcha.isNotEmpty) 'captcha': captcha,
      if (direct) 'direct': true,
    }, device: true);
    return body['session_id'] as String? ?? '';
  }

  // ---------- 邮箱登录（P0-4） ----------

  Future<void> sendEmailCode(String email) async {
    await _post('/v1/auth/email/code', {'email': email});
  }

  Future<void> loginEmail(String email, String code) async {
    final body = await _post('/v1/auth/email/login', {
      'email': email,
      'code': code,
    });
    _token = body['token'] as String?;
    _authExpiredNotified = false;
    await _persist();
  }

  /// 确保本机有设备身份；没有就补一次 bind（服务端绑定是幂等的）。
  ///
  /// 历史快照里存在「令牌在、device_id 却为空」的情况（早期版本先落盘后绑设备），
  /// 被控开关直接拿它去 prepare 会得到 Rust 的
  /// `Bad state: missing session (token/device_id)`。这里先自愈，再决定是否报错。
  Future<DeviceInfo> ensureDevice({String platform = 'unknown'}) async {
    final token = _token;
    if (token == null || token.isEmpty) {
      _notifyAuthExpired();
      throw ApiException('AUTH_TOKEN_EXPIRED', '登录已过期，请重新登录');
    }
    final id = _deviceId ?? '';
    final code = _deviceCode ?? '';
    final deviceToken = _deviceToken ?? '';
    if (id.isNotEmpty && code.isNotEmpty && deviceToken.isNotEmpty) {
      return DeviceInfo(
        id: id,
        code: code,
        platform: (_devicePlatform ?? '').isNotEmpty
            ? _devicePlatform!
            : platform,
        online: true,
      );
    }
    return bindDevice(platform);
  }

  // ---------- 运行时配置（P1-1 / P1-4） ----------

  /// 读取服务端下发的白名单配置；失败时由调用方决定回落默认值。
  Future<AppConfig> fetchConfig() async {
    final body = await _get('/v1/config');
    final cfg = body['config'] as Map<String, dynamic>? ?? const {};
    return AppConfig.fromJson(cfg);
  }

  // ---------- 出口地址（P1-3） ----------

  /// 服务端看到的出口地址（TCP 源地址），用于诊断页展示。
  Future<ReflectInfo> netReflect() async {
    final body = await _get('/v1/net/reflect');
    return ReflectInfo.fromJson(body);
  }

  // ---------- 协助码（P0-3） ----------

  /// 被控端生成 6 位协助码（需要本机设备凭证）。
  Future<String> createAssistCode() async {
    final body = await _post('/v1/assist/create', {
      'device_code': _ownDeviceCode,
    }, device: true);
    return body['code'] as String? ?? '';
  }

  /// 主控兑换协助码，返回会话 ID。协助码本身就是授权，不需要 captcha。
  Future<String> redeemAssistCode(String code) async {
    final body = await _post('/v1/assist/redeem', {'code': code}, device: true);
    return body['session_id'] as String? ?? '';
  }

  /// 本机设备码：协助码生成接口要求显式传 device_code；
  /// 会话页的「连接信息 · 本机设备码」也复用它（未绑定时为空）。
  String _ownDeviceCode = '';

  String get ownDeviceCode => _ownDeviceCode;

  set ownDeviceCode(String code) => _ownDeviceCode = code;

  // ---------- 文件传输（P0-1 / P0-2） ----------

  Future<List<FileInfo>> listFiles() async {
    final body = await _get('/v1/files');
    final list = body['files'] as List<dynamic>? ?? const [];
    return list
        .map((e) => FileInfo.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  /// multipart 上传。单文件上限由服务端配置决定（默认 32 MiB）。
  ///
  /// [control] 传了就按字节流回传进度、并支持真正的「暂停」：
  /// 请求体是自建的可停顿流，暂停后不再向连接写入新字节，恢复后从断点继续，
  /// 既不是假装暂停，也不会重传已发出的部分。
  Future<FileInfo> uploadFile(
    String name,
    Uint8List bytes, {
    UploadControl? control,
  }) async {
    final req = http.MultipartRequest('POST', Uri.parse('$baseUrl/v1/files'));
    if (_token != null) req.headers['authorization'] = 'Bearer $_token';
    if (_deviceToken != null) req.headers['x-device-token'] = _deviceToken!;
    req.files.add(http.MultipartFile.fromBytes('file', bytes, filename: name));
    final tracked = control == null ? req : _TrackedMultipart(req, control);
    final streamed = await _client.send(tracked);
    final res = await http.Response.fromStream(streamed);
    final body = _decode(res);
    final file = body['file'] as Map<String, dynamic>? ?? const {};
    return FileInfo.fromJson(file);
  }

  Future<void> deleteFile(String id) async {
    final res = await _client.delete(
      Uri.parse('$baseUrl/v1/files/$id'),
      headers: _headers(),
    );
    _decode(res);
  }

  /// 换一张一次性下载票据（浏览器/桌面下载都不需要再带请求头）。
  Future<String> downloadTicket(String id) async {
    final body = await _post(
      '/v1/files/$id/ticket',
      const <String, dynamic>{},
      device: true,
    );
    final url = body['url'] as String? ?? '';
    if (url.isEmpty) return '';
    return url.startsWith('http') ? url : '$baseUrl$url';
  }

  /// 带凭证下载文件字节（桌面端落盘用）。返回 403/404 时照常抛 [ApiException]。
  Future<Uint8List> downloadFile(String id) async {
    final res = await _client.get(
      Uri.parse('$baseUrl/v1/files/$id'),
      headers: _headers(device: true),
    );
    if (res.statusCode >= 400) {
      _decode(res); // 统一走错误信封解析后抛出
      throw ApiException('UNKNOWN', '下载失败（HTTP ${res.statusCode}）');
    }
    return res.bodyBytes;
  }

  /// 探测到网关的往返时延（诊断页用真实测量，不打桩）。
  Future<Duration?> ping() async {
    final sw = Stopwatch()..start();
    try {
      final res = await _client.get(Uri.parse('$baseUrl/healthz'));
      sw.stop();
      if (res.statusCode >= 500) return null;
      return sw.elapsed;
    } catch (_) {
      sw.stop();
      return null;
    }
  }

  Future<SessionStatus> sessionStatus(String sessionId) async {
    final body = await _get(
      '/v1/connect/status?session_id=$sessionId',
      device: true,
    );
    final state = switch (body['state'] as String? ?? '') {
      'requested' => SessionState.waitingConsent,
      'authorized' => SessionState.authorized,
      'active' => SessionState.active,
      'ended' => SessionState.ended,
      _ => SessionState.idle,
    };
    return SessionStatus(state, message: body['reason'] as String? ?? '');
  }
}

/// 上传进度与暂停控制（P2-2 传输队列）。
///
/// 只影响「写入请求体的节奏」：暂停时不向连接写新字节，服务端看到的是慢速客户端；
/// 恢复后从断点续写，不重传已发出的字节。总长度是 multipart 全长（含边界），
/// 所以百分比与 bytes 计数含少量协议开销，用于队列展示足够精确。
class UploadControl {
  UploadControl({this.onProgress});

  /// 每写出一块就回调一次：(已发送字节, 总字节)。
  final void Function(int sent, int total)? onProgress;

  bool _paused = false;
  Completer<void>? _resume;

  bool get paused => _paused;

  void pause() {
    if (_paused) return;
    _paused = true;
    _resume = Completer<void>();
  }

  void resume() {
    if (!_paused) return;
    _paused = false;
    final gate = _resume;
    _resume = null;
    if (gate != null && !gate.isCompleted) gate.complete();
  }

  /// 暂停时挂起，直到 [resume]；调用方在每块字节前等它。
  Future<void> waitIfPaused() async {
    while (_paused) {
      final gate = _resume ??= Completer<void>();
      await gate.future;
    }
  }
}

/// 把 [http.MultipartRequest] 的请求体重新包一层，插进进度回调和暂停闸门。
class _TrackedMultipart extends http.BaseRequest {
  _TrackedMultipart(this._inner, this._control) : super('POST', _inner.url) {
    headers.addAll(_inner.headers);
  }

  final http.MultipartRequest _inner;
  final UploadControl _control;

  @override
  int get contentLength => _inner.contentLength;

  @override
  http.ByteStream finalize() {
    super.finalize(); // 只标记已 finalize，请求体由下面这层包装产出。
    final total = _inner.contentLength;
    final raw = _inner.finalize();
    Stream<List<int>> body() async* {
      var sent = 0;
      await for (final chunk in raw) {
        await _control.waitIfPaused();
        sent += chunk.length;
        _control.onProgress?.call(sent, total);
        yield chunk;
      }
    }

    return http.ByteStream(body());
  }
}
