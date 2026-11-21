/// 本地会话快照：刷新页面 / 重启客户端后用来恢复登录态。
///
/// 只放恢复登录必需的东西：账号令牌 + 本机设备身份。
/// 不放手机号、邮箱、验证码——那些不进磁盘。
class StoredSession {
  const StoredSession({
    required this.token,
    required this.deviceToken,
    required this.deviceId,
    required this.deviceCode,
    required this.devicePlatform,
  });

  final String token;
  final String deviceToken;
  final String deviceId;
  final String deviceCode;
  final String devicePlatform;

  Map<String, dynamic> toJson() => <String, dynamic>{
    'token': token,
    'device_token': deviceToken,
    'device_id': deviceId,
    'device_code': deviceCode,
    'device_platform': devicePlatform,
  };

  /// 解析本地快照；字段缺失或类型不对一律返回 null（调用方按「没登录过」处理）。
  static StoredSession? fromJson(Map<String, dynamic>? json) {
    if (json == null) return null;
    final token = json['token'] as String? ?? '';
    if (token.isEmpty) return null;
    return StoredSession(
      token: token,
      deviceToken: json['device_token'] as String? ?? '',
      deviceId: json['device_id'] as String? ?? '',
      deviceCode: json['device_code'] as String? ?? '',
      devicePlatform: json['device_platform'] as String? ?? '',
    );
  }
}
