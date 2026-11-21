import 'dart:typed_data';

import 'package:flutter/foundation.dart'
    show TargetPlatform, defaultTargetPlatform, kIsWeb;

/// 本机平台名（服务端用来决定图标与能力）。
///
/// 之前散落在登录页里硬编码 `kIsWeb ? "web" : "macos"`，Windows 版会把自己
/// 绑定成 macOS 设备；统一到一处，登录/补绑被控都取同一个值。
String currentPlatformName() {
  if (kIsWeb) return 'web';
  return switch (defaultTargetPlatform) {
    TargetPlatform.windows => 'windows',
    TargetPlatform.macOS => 'macos',
    TargetPlatform.linux => 'linux',
    TargetPlatform.android => 'android',
    TargetPlatform.iOS => 'ios',
    _ => 'unknown',
  };
}

/// 设备与会话的最小模型（对接 gateway 的 JSON）。
class DeviceInfo {
  const DeviceInfo({
    required this.id,
    required this.code,
    required this.platform,
    required this.online,
  });

  final String id;
  final String code;
  final String platform;
  final bool online;

  factory DeviceInfo.fromJson(Map<String, dynamic> json) => DeviceInfo(
    id: json['id'] as String? ?? json['device_id'] as String? ?? '',
    // 列表接口给 raw_code（无空格）或 device_code（3-3-3），统一成 9 位数字。
    code:
        ((json['raw_code'] as String?) ??
                (json['device_code'] as String? ?? ''))
            .replaceAll(' ', ''),
    platform: json['platform'] as String? ?? 'unknown',
    online: json['online'] as bool? ?? false,
  );

  /// 展示用：3-3-3 分组
  String get prettyCode {
    if (code.length != 9) return code;
    return '${code.substring(0, 3)} ${code.substring(3, 6)} ${code.substring(6)}';
  }
}

/// 一个已上传文件的元数据（对接 /v1/files）。
class FileInfo {
  const FileInfo({
    required this.id,
    required this.name,
    required this.size,
    required this.sha256,
    required this.createdAt,
  });

  final String id;
  final String name;
  final int size;
  final String sha256;
  final DateTime? createdAt;

  factory FileInfo.fromJson(Map<String, dynamic> json) => FileInfo(
    id: json['id'] as String? ?? '',
    name: json['name'] as String? ?? 'file',
    size: (json['size'] as num?)?.toInt() ?? 0,
    sha256: json['sha256'] as String? ?? '',
    createdAt: DateTime.tryParse(json['created_at'] as String? ?? '')
        ?.toLocal(),
  );

  /// 人类可读大小（B / KiB / MiB）。
  String get prettySize {
    if (size < 1024) return '$size B';
    if (size < 1024 * 1024) return '${(size / 1024).toStringAsFixed(1)} KiB';
    return '${(size / (1024 * 1024)).toStringAsFixed(2)} MiB';
  }
}

/// 用户在本机选中的文件（P2-2 本机栏）。
///
/// Web 由浏览器文件选择器给出（[file_pick_web.dart]），桌面端不提供选择器，
/// 页面改走「文本文件」入口，所以这个模型只承载「名字 + 字节」。
class PickedLocalFile {
  const PickedLocalFile(this.name, this.bytes);

  final String name;
  final Uint8List bytes;

  int get size => bytes.length;
}

/// 服务端下发的运行时配置（/v1/config，白名单键）。
class AppConfig {
  const AppConfig({
    this.sessionMaxMinutes = 720,
    this.fileMaxBytes = 33554432,
    this.diagAutoRefreshSecs = 30,
    this.forceUpdate = false,
  });

  final int sessionMaxMinutes;
  final int fileMaxBytes;
  final int diagAutoRefreshSecs;
  final bool forceUpdate;

  factory AppConfig.fromJson(Map<String, dynamic> json) {
    final session = (json['session'] as Map<String, dynamic>?) ?? const {};
    final file = (json['file'] as Map<String, dynamic>?) ?? const {};
    final diag = (json['diag'] as Map<String, dynamic>?) ?? const {};
    final client = (json['client'] as Map<String, dynamic>?) ?? const {};
    return AppConfig(
      sessionMaxMinutes: (session['max_minutes'] as num?)?.toInt() ?? 720,
      fileMaxBytes: (file['max_bytes'] as num?)?.toInt() ?? 33554432,
      diagAutoRefreshSecs: (diag['auto_refresh_secs'] as num?)?.toInt() ?? 30,
      forceUpdate: client['force_update'] as bool? ?? false,
    );
  }
}

/// 服务端观察到的出口地址（/v1/net/reflect）。
class ReflectInfo {
  const ReflectInfo({required this.ip, required this.port, this.source = ''});

  final String ip;
  final int port;
  final String source;

  factory ReflectInfo.fromJson(Map<String, dynamic> json) => ReflectInfo(
    ip: json['ip'] as String? ?? '',
    port: (json['port'] as num?)?.toInt() ?? 0,
    source: json['source'] as String? ?? '',
  );

  String get pretty => port == 0 ? ip : '$ip:$port';
}

enum SessionState {
  idle,
  requesting,
  waitingConsent,
  connecting,

  /// 对方已授权、授权窗口未过期（/v1/relay/ticket 可签发）。
  /// HTTP 网关能推进到的最后一态；再往后的 active 由桌面端核心回写。
  authorized,
  active,
  ended,
  failed,
}

class SessionStatus {
  const SessionStatus(this.state, {this.message = ''});
  final SessionState state;
  final String message;

  bool get isActive => state == SessionState.active;

  /// 「通道已建立」：已授权或已进入 active。
  /// Web 预览下服务端最多到 authorized，页面据此显示全屏占位而不是空白等待。
  bool get isLive => state == SessionState.active || state == SessionState.authorized;
}
