import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:flutter/material.dart';

import 'api.dart';
import 'models.dart';
import 'pages/login_page.dart';
import 'prefs.dart';
import 'shell.dart';
import 'theme.dart';

/// 全局导航键：登录过期时一次清空栈回登录页（包括压在首页之上的会话页）。
final GlobalKey<NavigatorState> erNavigatorKey = GlobalKey<NavigatorState>();

/// 显式指定的网关地址；未指定时为空串。
const String _erGatewayDefine = String.fromEnvironment('ER_GATEWAY');

/// 网关地址。
///
/// - Web（浏览器）：默认**空串**，所有请求走相对路径（/v1/...），由同源代理
///   scripts/serve-client-web.mjs 转发到一体化服务端，浏览器不会触发 CORS。
///   浏览器里绝不能烧绝对地址，否则 8091 页面请求 8090 就是跨源，预检必被拦。
/// - 原生桌面：默认本机 8080，可用 --dart-define=ER_GATEWAY=http://127.0.0.1:8090 覆盖。
final String erGatewayBaseUrl = _erGatewayDefine.isNotEmpty
    ? _erGatewayDefine
    : (kIsWeb ? '' : 'http://127.0.0.1:8080');

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  // 偏好必须在第一帧之前读出来：否则设置页/侧边栏会先闪一遍默认值，
  // 用户改过的主题、画质、开关也永远回不到上次的状态。
  await loadAndApplyPrefs();
  // 调试/截图用：?theme=light|dark|system 覆盖初始外观（不写持久化，刷新即回默认）。
  final q = Uri.base.queryParameters['theme'];
  if (q != null) {
    erThemeMode.value = themeModeFromPrefs(q);
  }
  runApp(ErApp(client: GatewayClient(baseUrl: erGatewayBaseUrl)));
}

class ErApp extends StatefulWidget {
  const ErApp({super.key, required this.client});

  final GatewayClient client;

  @override
  State<ErApp> createState() => _ErAppState();
}

class _ErAppState extends State<ErApp> {
  @override
  void initState() {
    super.initState();
    widget.client.authExpired.addListener(_onAuthExpired);
  }

  @override
  void didUpdateWidget(covariant ErApp oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.client != widget.client) {
      oldWidget.client.authExpired.removeListener(_onAuthExpired);
      widget.client.authExpired.addListener(_onAuthExpired);
    }
  }

  @override
  void dispose() {
    widget.client.authExpired.removeListener(_onAuthExpired);
    super.dispose();
  }

  /// 任意接口发现令牌失效 → 清空导航栈回登录页。
  ///
  /// 用 pushAndRemoveUntil 而不是让各页面自己 pop：过期可能发生在会话页、
  /// 也可能发生在二级弹窗背后，逐层修只会漏。
  void _onAuthExpired() {
    if (!mounted) return;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      final nav = erNavigatorKey.currentState;
      if (nav == null) return;
      nav.pushAndRemoveUntil(
        MaterialPageRoute<void>(
          builder: (_) => LoginPage(client: widget.client),
        ),
        (route) => false,
      );
    });
  }

  @override
  Widget build(BuildContext context) {
    return ValueListenableBuilder<ThemeMode>(
      valueListenable: erThemeMode,
      builder: (context, mode, _) => MaterialApp(
        navigatorKey: erNavigatorKey,
        title: 'EasyRemote',
        debugShowCheckedModeBanner: false,
        theme: buildTheme(Brightness.light),
        darkTheme: buildTheme(Brightness.dark),
        themeMode: mode,
        home: _SessionGate(client: widget.client),
      ),
    );
  }
}

/// 启动闸门：先拿本地会话恢复登录态，再决定进主界面还是登录页。
///
/// 没有这一层，浏览器一刷新（内存里的令牌清零、main() 又固定 home=LoginPage）
/// 就必然被踢回登录页——这就是「登录了刷新又回登录」的直接原因。
class _SessionGate extends StatefulWidget {
  const _SessionGate({required this.client});

  final GatewayClient client;

  @override
  State<_SessionGate> createState() => _SessionGateState();
}

class _SessionGateState extends State<_SessionGate> {
  late final Future<DeviceInfo?> _restore = widget.client.restoreSession();

  @override
  Widget build(BuildContext context) {
    return FutureBuilder<DeviceInfo?>(
      future: _restore,
      builder: (context, snapshot) {
        if (snapshot.connectionState != ConnectionState.done) {
          return const _BootSplash();
        }
        final device = snapshot.data;
        if (device == null) return LoginPage(client: widget.client);
        return HomeShell(client: widget.client, device: device);
      },
    );
  }
}

/// 恢复登录态期间的占位：避免先闪一下登录页再跳走。
class _BootSplash extends StatelessWidget {
  const _BootSplash();

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Scaffold(
      body: Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              Icons.desktop_windows_rounded,
              size: 40,
              color: scheme.primary,
            ),
            const SizedBox(height: 14),
            Text('EasyRemote', style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 18),
            const SizedBox(
              width: 20,
              height: 20,
              child: CircularProgressIndicator(strokeWidth: 2),
            ),
          ],
        ),
      ),
    );
  }
}
