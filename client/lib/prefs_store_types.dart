// 本机偏好：外观 / 画质 / 安全开关。
//
// 与 session_store 分开：会话是「登录态」（换浏览器就该失效），偏好是「这台设备的
// 使用习惯」（刷新、重开都该保留）。两者都是纯本地数据，不上传服务端。
class Prefs {
  const Prefs({
    this.themeMode = 'system',
    this.quality = '自动',
    this.privacyScreen = false,
    this.preventSleep = true,
  });

  /// system | light | dark
  final String themeMode;
  final String quality;
  final bool privacyScreen;
  final bool preventSleep;

  Prefs copyWith({
    String? themeMode,
    String? quality,
    bool? privacyScreen,
    bool? preventSleep,
  }) =>
      Prefs(
        themeMode: themeMode ?? this.themeMode,
        quality: quality ?? this.quality,
        privacyScreen: privacyScreen ?? this.privacyScreen,
        preventSleep: preventSleep ?? this.preventSleep,
      );

  Map<String, dynamic> toJson() => <String, dynamic>{
        'theme_mode': themeMode,
        'quality': quality,
        'privacy_screen': privacyScreen,
        'prevent_sleep': preventSleep,
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
        privacyScreen: json['privacy_screen'] is bool ? json['privacy_screen'] as bool : false,
        preventSleep: json['prevent_sleep'] is bool ? json['prevent_sleep'] as bool : true,
      );
}
