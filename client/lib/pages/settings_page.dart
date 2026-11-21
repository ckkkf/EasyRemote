import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../models.dart';
import '../shell.dart';
import '../theme.dart';

/// 设置页：外观（浅色/深色/跟随系统）、画质、安全与设备信息。
class SettingsPage extends StatefulWidget {
  const SettingsPage({super.key, required this.device, this.config, this.onLogout});

  final DeviceInfo device;

  /// 退出登录回调；为空时不显示该入口（例如被嵌到不支持登出的容器里）。
  final VoidCallback? onLogout;

  /// 服务端下发的运行参数（/v1/config）。为空表示还没拿到，用默认值展示。
  final AppConfig? config;

  @override
  State<SettingsPage> createState() => _SettingsPageState();
}

class _SettingsPageState extends State<SettingsPage> {
  bool _privacyScreen = false;
  bool _preventSleep = true;
  String _quality = '自动';

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    final wide = MediaQuery.sizeOf(context).width >= ErTokens.mobileBreakpoint;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const PageHeader(title: '设置', subtitle: '外观、画质、安全与本机信息'),
        Expanded(
          child: ListView(
            padding: EdgeInsets.fromLTRB(wide ? 32 : 16, 0, wide ? 32 : 16, 32),
            children: [
              if (widget.config?.forceUpdate ?? false) ...[
                Container(
                  padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
                  decoration: BoxDecoration(
                    color: scheme.errorContainer,
                    borderRadius: BorderRadius.circular(ErTokens.radiusMd),
                  ),
                  child: Row(
                    children: [
                      Icon(Icons.system_update_alt_rounded, size: 16, color: scheme.onErrorContainer),
                      const SizedBox(width: 8),
                      Expanded(
                        child: Text('服务端要求强制更新：请更新到最新版客户端后再连接。',
                            style: text.bodySmall?.copyWith(color: scheme.onErrorContainer)),
                      ),
                    ],
                  ),
                ),
                const SizedBox(height: 16),
              ],
              _Section(
                title: '外观',
                subtitle: '主题跟随系统时会随系统深色模式自动切换',
                children: [
                  Padding(
                    padding: const EdgeInsets.fromLTRB(16, 4, 16, 12),
                    child: Row(
                      children: [
                        Expanded(child: Text('主题', style: text.bodyMedium)),
                        const ThemeModeSwitch(),
                      ],
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 16),
              _Section(
                title: '画质',
                subtitle: '自动档会按网络状况实时调整',
                children: [
                  Padding(
                    padding: const EdgeInsets.fromLTRB(16, 0, 16, 12),
                    child: Row(
                      children: [
                        Expanded(child: Text('清晰度', style: text.bodyMedium)),
                        DropdownButton<String>(
                          value: _quality,
                          underline: const SizedBox.shrink(),
                          borderRadius: BorderRadius.circular(ErTokens.radiusMd),
                          items: const [
                            DropdownMenuItem(value: '自动', child: Text('自动')),
                            DropdownMenuItem(value: '高清', child: Text('高清')),
                            DropdownMenuItem(value: '流畅', child: Text('流畅')),
                          ],
                          onChanged: (v) => setState(() => _quality = v ?? '自动'),
                        ),
                      ],
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 16),
              _Section(
                title: '安全',
                children: [
                  SwitchListTile(
                    value: _privacyScreen,
                    onChanged: (v) => setState(() => _privacyScreen = v),
                    title: const Text('隐私屏'),
                    subtitle: const Text('开启后被控端画面不离开本机（对方看到黑屏）'),
                  ),
                  const Divider(height: 1),
                  SwitchListTile(
                    value: _preventSleep,
                    onChanged: (v) => setState(() => _preventSleep = v),
                    title: const Text('会话期间阻止睡眠'),
                    subtitle: const Text('避免长时间无人操作时主机休眠断连'),
                  ),
                ],
              ),
              const SizedBox(height: 16),
              _Section(
                title: '运行参数',
                subtitle: '由服务端配置中心下发，本页只读；诊断页的自动刷新间隔同样取自这里',
                children: [
                  ListTile(
                    title: const Text('单次会话上限'),
                    trailing: Text('${widget.config?.sessionMaxMinutes ?? 720} 分钟', style: text.bodyMedium),
                  ),
                  const Divider(height: 1),
                  ListTile(
                    title: const Text('单文件上传上限'),
                    trailing: Text(_sizeText(widget.config?.fileMaxBytes ?? 33554432), style: text.bodyMedium),
                  ),
                  const Divider(height: 1),
                  ListTile(
                    title: const Text('诊断自动刷新'),
                    subtitle: const Text('每轮自动重新测量时延与丢包'),
                    trailing: Text('${widget.config?.diagAutoRefreshSecs ?? 30} 秒', style: text.bodyMedium),
                  ),
                ],
              ),
              const SizedBox(height: 16),
              _Section(
                title: '本机',
                children: [
                  ListTile(
                    title: const Text('本机设备码'),
                    subtitle: Text(widget.device.prettyCode,
                        style: text.titleSmall?.copyWith(letterSpacing: 2)),
                    trailing: IconButton(
                      tooltip: '复制',
                      icon: const Icon(Icons.copy_rounded, size: 18),
                      onPressed: () {
                        Clipboard.setData(ClipboardData(text: widget.device.prettyCode));
                        ScaffoldMessenger.of(context)
                            .showSnackBar(SnackBar(content: Text('已复制 ' + widget.device.prettyCode)));
                      },
                    ),
                  ),
                  const Divider(height: 1),
                  const ListTile(
                    title: Text('版本'),
                    subtitle: Text('EasyRemote 1.0.0 · 客户端'),
                    trailing: Icon(Icons.chevron_right_rounded, size: 18),
                  ),
                ],
              ),
              if (widget.onLogout != null) ...[
                const SizedBox(height: 16),
                _Section(
                  title: '账号',
                  subtitle: '登录态保存在本机，退出会清掉令牌快照',
                  children: [
                    ListTile(
                      key: const Key('logout-tile'),
                      leading: Icon(Icons.logout_rounded, size: 18, color: scheme.error),
                      title: Text('退出登录', style: text.bodyMedium?.copyWith(color: scheme.error)),
                      onTap: widget.onLogout,
                    ),
                  ],
                ),
              ],
              const SizedBox(height: 16),
              Row(
                children: [
                  Icon(Icons.shield_outlined, size: 15, color: scheme.onSurfaceVariant),
                  const SizedBox(width: 6),
                  Expanded(
                    child: Text('会话全程端到端加密，中继节点只转发密文。',
                        style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant)),
                  ),
                ],
              ),
            ],
          ),
        ),
      ],
    );
  }
}

/// 字节数转人类可读（MiB 为主）。
String _sizeText(int bytes) {
  if (bytes < 1024) return '$bytes B';
  if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(0)} KiB';
  return '${(bytes / (1024 * 1024)).toStringAsFixed(0)} MiB';
}

class _Section extends StatelessWidget {
  const _Section({required this.title, this.subtitle, required this.children});

  final String title;
  final String? subtitle;
  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    return Container(
      decoration: BoxDecoration(
        color: scheme.surfaceContainerLow,
        borderRadius: BorderRadius.circular(ErTokens.radiusLg),
        border: Border.all(color: scheme.outlineVariant.withValues(alpha: 0.7)),
      ),
      // ListTile 的水波纹要画在最近的 Material 上；本容器自带背景色，
      // 不垫一层透明 Material 就会命中 Flutter 的「背景色会遮住 ink」断言。
      child: Material(
        type: MaterialType.transparency,
        child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Padding(
            padding: EdgeInsets.fromLTRB(16, 14, 16, subtitle == null ? 0 : 4),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(title, style: text.titleSmall),
                if (subtitle != null)
                  Text(subtitle!, style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant)),
              ],
            ),
          ),
          const SizedBox(height: 8),
          ...children,
        ],
        ),
      ),
    );
  }
}
