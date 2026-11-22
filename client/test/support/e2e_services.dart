// E2E 测试环境：在测试进程内构建并启动真实 stack + relay，结束后回收。
//
// 设计取舍：
//   - 端口必须随机：固定 18090/19000 会和开发者本机服务/上一次失败残留冲突。
//   - 就绪只认 /healthz 200 与 relay 日志，不用固定 sleep 猜。
//   - 任何启动失败都抛异常（测试失败），不允许“缺服务就 skip”。
//   - 显式提供 ER_GATEWAY_URL / ER_RELAY_ADDR 时复用外部服务，方便调试。
import 'dart:async';
import 'dart:convert';
import 'dart:io';

/// 一次 E2E 运行依赖的服务集合。
class E2eServices {
  E2eServices._({
    required this.gatewayUrl,
    required this.relayAddr,
    required this.lossyRelayAddr,
    required this.external,
    Process? stack,
    Process? relay,
    Process? lossy,
    Directory? workDir,
    String? binDir,
    String? root,
    int? relayPort,
    Map<String, String>? env,
  }) : _stack = stack,
       _relay = relay,
       _lossy = lossy,
       _workDir = workDir,
       _binDir = binDir,
       _root = root,
       _relayPort = relayPort,
       _env = env;

  final String gatewayUrl;
  final String relayAddr;

  /// 经 lossyproxy（丢包/时延/乱序）的中继地址，用于弱网测试。
  final String lossyRelayAddr;
  final bool external;

  Process? _stack;
  Process? _relay;
  Process? _lossy;
  Directory? _workDir;
  final String? _binDir;
  final String? _root;
  final int? _relayPort;
  final Map<String, String>? _env;
  final StringBuffer _log = StringBuffer();
  int _relayListeningCount = 0;
  bool _stopped = false;

  /// 服务日志（启动失败时附在异常里）。
  String get log => _log.toString();

  static Future<E2eServices> start({
    Duration timeout = const Duration(seconds: 30),
    String? gatewayUrl,
    String? relayAddr,
  }) async {
    final envGw = gatewayUrl ?? Platform.environment['ER_GATEWAY_URL'];
    final envRelay = relayAddr ?? Platform.environment['ER_RELAY_ADDR'];
    if (envGw != null &&
        envRelay != null &&
        envGw.isNotEmpty &&
        envRelay.isNotEmpty) {
      // 外部服务模式：只做一次健康检查，不接管别人的进程。
      final probe = await _probeHealth(envGw, timeout);
      if (probe != 200) {
        throw StateError('外部 E2E 服务不可用：$envGw/healthz → $probe');
      }
      return E2eServices._(
        gatewayUrl: envGw,
        relayAddr: envRelay,
        // 外部服务模式不注入弱网代理，弱网用例退化为直连。
        lossyRelayAddr: envRelay,
        external: true,
      );
    }

    final root = _repoRoot();
    // 每个测试进程独立 bin 目录：多个 E2E 文件并发跑时不能同时写同一个二进制。
    final workDir = Directory.systemTemp.createTempSync('er-e2e-');
    final binDir = Directory('${workDir.path}/bin')
      ..createSync(recursive: true);
    await _build(root, binDir, timeout);

    final gatewayPort = await _freeTcpPort();
    final relayPort = await _freeUdpPort();
    final lossyPort = await _freeUdpPort();

    final env = <String, String>{
      ...Platform.environment,
      'ER_ENV': 'development',
      // 只给 harness 用的只读在线数探针；生产装配没有这个路由。
      'ER_E2E_HARNESS': '1',
    };

    final services = E2eServices._(
      gatewayUrl: 'http://127.0.0.1:$gatewayPort',
      relayAddr: '127.0.0.1:$relayPort',
      lossyRelayAddr: '127.0.0.1:$lossyPort',
      external: false,
      workDir: workDir,
      binDir: binDir.path,
      root: root.path,
      relayPort: relayPort,
      env: env,
    );

    services._stack = await Process.start(
      '${binDir.path}/stack',
      ['-addr', '127.0.0.1:$gatewayPort'],
      workingDirectory: root.path,
      environment: env,
    );
    services._pipeLog('stack', services._stack!);

    services._relay = await Process.start(
      '${binDir.path}/relay',
      ['-addr', '127.0.0.1:$relayPort'],
      workingDirectory: root.path,
      environment: env,
    );
    services._pipeLog('relay', services._relay!);

    try {
      // 进程可能立刻退出（端口占用/配置错误）：health 轮询会带上日志超时。
      await services._waitHealth(timeout);
      await services._waitLog('relay listening on', timeout);
      // 弱网代理：5% 丢包 + 2ms 抖动 + 2% 乱序，种子固定可复现。
      services._lossy = await Process.start(
        '${binDir.path}/lossyproxy',
        [
          '-listen',
          '127.0.0.1:$lossyPort',
          '-target',
          '127.0.0.1:$relayPort',
          '-loss',
          '0.05',
          '-delay',
          '2ms',
          '-jitter',
          '2ms',
          '-reorder',
          '0.02',
          '-seed',
          '7',
        ],
        workingDirectory: root.path,
        environment: env,
      );
      services._pipeLog('lossy', services._lossy!);
      await services._waitLog('lossyproxy ', timeout);
    } catch (_) {
      await services.stop();
      rethrow;
    }
    return services;
  }

  /// 健康检查（停止后返回 0，用于证明端口已释放）。
  Future<int> health() => _probeHealth(gatewayUrl, const Duration(seconds: 2));

  /// 重启真实 relay 进程（同端口），等待新进程打印 listening。
  /// 用于 ER-903b：证明 relay 崩溃后编排层可重新注册/恢复。
  Future<void> restartRelay({
    Duration timeout = const Duration(seconds: 15),
  }) async {
    if (external) {
      throw StateError('外部 relay 模式不支持重启');
    }
    final binDir = _binDir;
    final root = _root;
    final port = _relayPort;
    final env = _env;
    if (binDir == null || root == null || port == null || env == null) {
      throw StateError('E2E 服务缺少 relay 重启所需字段');
    }
    _relayListeningCount = 0;
    _relay?.kill(ProcessSignal.sigkill);
    await _relay?.exitCode;
    _relay = await Process.start(
      '$binDir/relay',
      ['-addr', '127.0.0.1:$port'],
      workingDirectory: root,
      environment: env,
    );
    _pipeLog('relay', _relay!);
    final deadline = DateTime.now().add(timeout);
    while (DateTime.now().isBefore(deadline)) {
      if (_relayListeningCount >= 1) return;
      await Future<void>.delayed(const Duration(milliseconds: 100));
    }
    throw TimeoutException('relay 未在重启后监听，日志：\n$log');
  }

  /// 等真实信令连接建立（被控端上线）：轮询 /debug/presence，不用固定 sleep。
  Future<void> waitForPresence(
    int minDevices, {
    Duration timeout = const Duration(seconds: 15),
  }) async {
    final deadline = DateTime.now().add(timeout);
    final client = HttpClient()
      ..connectionTimeout = const Duration(milliseconds: 500);
    try {
      while (DateTime.now().isBefore(deadline)) {
        try {
          final req = await client.getUrl(
            Uri.parse('$gatewayUrl/debug/presence'),
          );
          final res = await req.close();
          final body = await res.transform(utf8.decoder).join();
          if (res.statusCode == 200) {
            final decoded = jsonDecode(body) as Map<String, dynamic>;
            final data = decoded['data'] as Map<String, dynamic>? ?? const {};
            final devices = (data['devices'] as num?)?.toInt() ?? 0;
            if (devices >= minDevices) return;
          }
        } on SocketException {
          // 未就绪。
        }
        await Future<void>.delayed(const Duration(milliseconds: 100));
      }
    } finally {
      client.close(force: true);
    }
    throw TimeoutException('信令在线数未达到 $minDevices，日志：\n$log');
  }

  static Future<int> _probeHealth(String base, Duration timeout) async {
    final client = HttpClient()
      ..connectionTimeout = const Duration(milliseconds: 500);
    final deadline = DateTime.now().add(timeout);
    try {
      while (DateTime.now().isBefore(deadline)) {
        try {
          final req = await client.getUrl(Uri.parse('$base/healthz'));
          final res = await req.close();
          await res.drain<void>();
          if (res.statusCode == 200) return 200;
        } on SocketException {
          // 还没起来：继续轮询。
        }
        await Future<void>.delayed(const Duration(milliseconds: 100));
      }
      return 0;
    } finally {
      client.close(force: true);
    }
  }

  Future<void> _waitHealth(Duration timeout) async {
    final deadline = DateTime.now().add(timeout);
    while (DateTime.now().isBefore(deadline)) {
      if (_stackExited) {
        throw StateError('stack 提前退出，日志：\n$log');
      }
      final client = HttpClient()
        ..connectionTimeout = const Duration(milliseconds: 500);
      try {
        final req = await client.getUrl(Uri.parse('$gatewayUrl/healthz'));
        final res = await req.close();
        await res.drain<void>();
        if (res.statusCode == 200) return;
      } on SocketException {
        // 未就绪。
      } finally {
        client.close(force: true);
      }
      await Future<void>.delayed(const Duration(milliseconds: 100));
    }
    throw TimeoutException('stack 未在 $timeout 内就绪，日志：\n$log');
  }

  bool _stackExited = false;

  void _pipeLog(String tag, Process p) {
    p.stdout.transform(utf8.decoder).listen((s) {
      _log.write('[$tag] $s');
      if (tag == 'relay' && s.contains('relay listening on')) {
        _relayListeningCount++;
      }
    });
    p.stderr.transform(utf8.decoder).listen((s) {
      _log.write('[$tag!] $s');
      if (tag == 'relay' && s.contains('relay listening on')) {
        _relayListeningCount++;
      }
    });
    p.exitCode.then((code) {
      _stackExited = _stackExited || tag == 'stack';
      _log.write('[$tag] exited code=$code\n');
    });
  }

  Future<void> _waitLog(String needle, Duration timeout) async {
    final deadline = DateTime.now().add(timeout);
    while (DateTime.now().isBefore(deadline)) {
      if (_log.toString().contains(needle)) return;
      if (_relay == null) break;
      await Future<void>.delayed(const Duration(milliseconds: 50));
    }
    throw TimeoutException('未在 $timeout 内看到日志 "$needle"，日志：\n$log');
  }

  /// 幂等停止：杀掉两个子进程并等待退出。
  Future<void> stop() async {
    if (_stopped) return;
    _stopped = true;
    for (final p in [_lossy, _relay, _stack]) {
      if (p == null) continue;
      p.kill(ProcessSignal.sigkill);
      await p.exitCode.timeout(const Duration(seconds: 5), onTimeout: () => -1);
    }
    _lossy = null;
    _relay = null;
    _stack = null;
    try {
      _workDir?.deleteSync(recursive: true);
    } catch (_) {
      // 临时目录清理失败不影响断言。
    }
  }

  static Directory _repoRoot() {
    var dir = Directory.current;
    for (var i = 0; i < 8; i++) {
      if (File('${dir.path}/server/go.mod').existsSync()) return dir;
      final parent = dir.parent;
      if (parent.path == dir.path) break;
      dir = parent;
    }
    throw StateError('找不到仓库根（server/go.mod），当前目录：${Directory.current.path}');
  }

  static Future<void> _build(
    Directory root,
    Directory binDir,
    Duration timeout,
  ) async {
    for (final cmd in ['stack', 'relay', 'lossyproxy']) {
      final res = await Process.run('go', [
        'build',
        '-o',
        '${binDir.path}/$cmd',
        './cmd/$cmd',
      ], workingDirectory: '${root.path}/server').timeout(timeout);
      if (res.exitCode != 0) {
        throw StateError(
          'go build ./cmd/$cmd 失败（exit ${res.exitCode}）：\n${res.stderr}',
        );
      }
    }
  }

  static Future<int> _freeTcpPort() async {
    final socket = await ServerSocket.bind(InternetAddress.loopbackIPv4, 0);
    final port = socket.port;
    await socket.close();
    return port;
  }

  static Future<int> _freeUdpPort() async {
    final socket = await RawDatagramSocket.bind(
      InternetAddress.loopbackIPv4,
      0,
    );
    final port = socket.port;
    socket.close();
    return port;
  }
}
