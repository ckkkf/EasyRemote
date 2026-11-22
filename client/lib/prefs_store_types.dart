// 本机偏好：外观 / 画质 / 安全开关。
//
// 与 session_store 分开：会话是「登录态」（换浏览器就该失效），偏好是「这台设备的
// 使用习惯」（刷新、重开都该保留）。两者都是纯本地数据，不上传服务端。
class Prefs {
  /// 偏好文件 schema 版本。旧版本（无 version 字段）的 allow_control=true 是
  /// “历史默认值”而不是用户选择，必须迁移为 false。
  static const int schemaVersion = 2;

  const Prefs({
    this.themeMode = 'system',
    this.quality = '自动',
    this.privacyScreen = false,
    this.preventSleep = true,
    this.allowControl = false,
    this.sameAccountDirect = false,
  });

  /// system | light | dark
  final String themeMode;
  final String quality;
  final bool privacyScreen;
  final bool preventSleep;

  /// 允许被控（本机共享）：默认关。
  ///
  /// 安全红线（docs/spec/pages.md C13）：首次安装不得默认暴露远控能力。
  /// 历史快照缺少该字段时也按 false 迁移，禁止 fail-open。
  final bool allowControl;

  /// 同账号设备免密直连：默认关。
  ///
  /// 关着更安全（点击连接会要求对方设备的临时密码）；打开后同一账号下
  /// 已登录设备点一下就直接发起，不再输入密码。
  final bool sameAccountDirect;

  Prefs copyWith({
    String? themeMode,
    String? quality,
    bool? privacyScreen,
    bool? preventSleep,
    bool? allowControl,
    bool? sameAccountDirect,
  }) => Prefs(
    themeMode: themeMode ?? this.themeMode,
    quality: quality ?? this.quality,
    privacyScreen: privacyScreen ?? this.privacyScreen,
    preventSleep: preventSleep ?? this.preventSleep,
    allowControl: allowControl ?? this.allowControl,
    sameAccountDirect: sameAccountDirect ?? this.sameAccountDirect,
  );

  Map<String, dynamic> toJson() => <String, dynamic>{
    'version': schemaVersion,
    'theme_mode': themeMode,
    'quality': quality,
    'privacy_screen': privacyScreen,
    'prevent_sleep': preventSleep,
    'allow_control': allowControl,
    'same_account_direct': sameAccountDirect,
  };

  /// 解析失败/字段缺失一律回落到默认值：偏好坏了不能挡住启动。
  static Prefs fromJson(Map<String, dynamic> json) => Prefs(
    themeMode: switch (json['theme_mode'] as String?) {
      'light' => 'light',
      'dark' => 'dark',
      _ => 'system',
    },
    quality: switch (json['quality'] as String?) {
      '高清' => '高清',
      '流畅' => '流畅',
      _ => '自动',
    },
    // 用 is bool 而不是 as bool?：脏数据是字符串时 as 会抛，偏好坏了不能挡住启动
    privacyScreen: json['privacy_screen'] is bool
        ? json['privacy_screen'] as bool
        : false,
    preventSleep: json['prevent_sleep'] is bool
        ? json['prevent_sleep'] as bool
        : true,
    // schema v2 起：旧快照里的 allow_control=true 是历史默认值（当时默认开），
    // 不能当成用户同意；只有带 version>=2 的显式 true 才保留。
    allowControl:
        (json['version'] is int && (json['version'] as int) >= schemaVersion) &&
            json['allow_control'] is bool
        ? json['allow_control'] as bool
        : false,
    sameAccountDirect: json['same_account_direct'] is bool
        ? json['same_account_direct'] as bool
        : false,
  );
}
