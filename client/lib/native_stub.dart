// Web 环境替身：真实采集/编解码/输入注入依赖 dart:ffi，浏览器不支持。
// 仅在 self_test_page 的「会话自检」里会用到，其余页面走 HTTP 网关接口，不受影响。
class ErHostLink {
  static ErHostLink load() => ErHostLink._();

  ErHostLink._();

  Map<String, dynamic> prepare({
    required String gatewayUrl,
    required String relayAddr,
    required String phone,
    String smsCode = '123456',
  }) => <String, dynamic>{
    'ok': false,
    'error': 'web 预览环境不支持原生采集（需要桌面端 er_ffi）',
  };

  Map<String, dynamic> serve({
    required String hostId,
    required String signalBase,
    int frames = 5,
    int timeoutSecs = 30,
  }) => <String, dynamic>{'ok': false, 'error': 'web 预览环境不支持被控端服务'};

  Map<String, dynamic> connect({
    required String gatewayUrl,
    required String relayAddr,
    required String phone,
    required String deviceCode,
    required String captcha,
    String smsCode = '123456',
    int frames = 5,
  }) => <String, dynamic>{'ok': false, 'error': 'web 预览环境不支持原生连接'};

  bool release(String hostId) => true;
}
