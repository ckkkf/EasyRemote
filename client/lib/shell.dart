import 'package:flutter/material.dart';

import 'api.dart';
import 'models.dart';
import 'pages/connect_page.dart';
import 'pages/device_list_page.dart';
import 'pages/diagnostics_page.dart';
import 'pages/login_page.dart';
import 'pages/files_page.dart';
import 'pages/self_test_page.dart';
import 'pages/session_page.dart';
import 'pages/settings_page.dart';
import 'theme.dart';

/// 登录后的主框架：桌面端左侧带文字的导航栏，窄屏切换为底部导航。
class HomeShell extends StatefulWidget {
  const HomeShell({super.key, required this.client, required this.device});

  final GatewayClient client;
  final DeviceInfo device;

  @override
  State<HomeShell> createState() => _HomeShellState();
}

class _ErNavItem {
  const _ErNavItem(this.icon, this.label, this.hint);
  final IconData icon;
  final String label;
  final String hint;
}

const _navItems = <_ErNavItem>[
  _ErNavItem(Icons.devices_rounded, '我的设备', '已绑定设备与本机设备码'),
  _ErNavItem(Icons.pin_rounded, '连接设备', '用 9 位设备码请求远程控制'),
  _ErNavItem(Icons.monitor_heart_rounded, '网络诊断', '链路质量与可执行建议'),
  _ErNavItem(Icons.tune_rounded, '设置', '外观、画质与安全'),
  _ErNavItem(Icons.science_rounded, '会话自检', '走真实服务与真实采集'),
  _ErNavItem(Icons.folder_rounded, '文件传输', '上传、下载与删除共享文件'),
];

class _HomeShellState extends State<HomeShell> {
  int _index = 0;
  String _connectPrefill = '';
  final List<String> _recents = <String>[];

  /// 服务端下发的运行时配置（P1-1 / P1-4）。拉不到就用默认值，不让配置抖动影响主流程。
  AppConfig? _config;

  @override
  void initState() {
    super.initState();
    // 协助码生成接口要求显式带 device_code，登录绑定设备后就把本机码交给客户端。
    widget.client.ownDeviceCode = widget.device.code;
    _loadConfig();
  }

  Future<void> _loadConfig() async {
    try {
      final cfg = await widget.client.fetchConfig();
      if (!mounted) return;
      setState(() => _config = cfg);
    } catch (_) {
      // 配置是增强项：失败保持默认值，页面自己会再刷新。
    }
  }

  void _goToConnect(String code) {
    setState(() {
      _connectPrefill = code;
      _index = 1;
    });
  }

  void _rememberCode(String code) {
    if (code.isEmpty) return;
    setState(() {
      _recents.remove(code);
      _recents.insert(0, code);
      if (_recents.length > 4) _recents.removeLast();
    });
  }

  Future<void> _openSession(String sessionId, String targetCode) async {
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => SessionPage(
          client: widget.client,
          sessionId: sessionId,
          targetCode: targetCode,
          ownDevice: widget.device,
          // 协助码接入没有目标设备码：会话页把它当「临时授权」显示。
          assist: targetCode.isEmpty,
          onLeave: () => Navigator.of(context).maybePop(),
        ),
      ),
    );
  }

  /// 退出登录：清掉本地会话快照，回到登录页。
  Future<void> _logout() async {
    await widget.client.logout();
    if (!mounted) return;
    Navigator.of(context).pushReplacement(
      MaterialPageRoute<void>(builder: (_) => LoginPage(client: widget.client)),
    );
  }

  List<Widget> get _pages => <Widget>[
        DeviceListPage(
          client: widget.client,
          ownDevice: widget.device,
          onConnect: _goToConnect,
        ),
        ConnectPage(
          client: widget.client,
          initialCode: _connectPrefill,
          recents: _recents,
          onCodeUsed: _rememberCode,
          onSessionStarted: _openSession,
        ),
        DiagnosticsPage(client: widget.client),
        SettingsPage(device: widget.device, config: _config, onLogout: _logout),
        const SelfTestPage(),
        FilesPage(client: widget.client, config: _config),
      ];

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final wide = MediaQuery.sizeOf(context).width >= ErTokens.mobileBreakpoint;

    if (!wide) {
      return Scaffold(
        body: SafeArea(bottom: false, child: _pages[_index]),
        bottomNavigationBar: NavigationBar(
          selectedIndex: _index,
          onDestinationSelected: (i) => setState(() => _index = i),
          destinations: [
            for (final item in _navItems)
              NavigationDestination(icon: Icon(item.icon, size: 22), label: item.label),
          ],
        ),
      );
    }

    return Scaffold(
      body: Row(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _Sidebar(
            index: _index,
            onSelect: (i) => setState(() => _index = i),
            ownDevice: widget.device,
          ),
          VerticalDivider(width: 1, color: scheme.outlineVariant.withValues(alpha: 0.6)),
          Expanded(child: _pages[_index]),
        ],
      ),
    );
  }
}

class _Sidebar extends StatelessWidget {
  const _Sidebar({required this.index, required this.onSelect, required this.ownDevice});

  final int index;
  final ValueChanged<int> onSelect;
  final DeviceInfo ownDevice;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;

    return Container(
      width: ErTokens.railWidth,
      color: scheme.surfaceContainerLow,
      padding: const EdgeInsets.fromLTRB(12, 16, 12, 16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
            child: Row(
              children: [
                Container(
                  width: 36,
                  height: 36,
                  decoration: BoxDecoration(
                    gradient: const LinearGradient(
                      colors: [Color(0xFF1E4BDB), Color(0xFF4C6FE8)],
                      begin: Alignment.topLeft,
                      end: Alignment.bottomRight,
                    ),
                    borderRadius: BorderRadius.circular(11),
                  ),
                  alignment: Alignment.center,
                  child: Text('ER', style: text.labelLarge?.copyWith(color: Colors.white, letterSpacing: 0.5)),
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text('EasyRemote', style: text.titleMedium),
                      Text('v1.0.0', style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant)),
                    ],
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(height: 20),
          Expanded(
            child: ListView.separated(
              itemCount: _navItems.length,
              separatorBuilder: (_, _) => const SizedBox(height: 4),
              itemBuilder: (context, i) {
                final item = _navItems[i];
                return _NavButton(
                  icon: item.icon,
                  label: item.label,
                  selected: i == index,
                  onTap: () => onSelect(i),
                );
              },
            ),
          ),
          const SizedBox(height: 12),
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 10),
            decoration: BoxDecoration(
              color: scheme.surfaceContainer,
              borderRadius: BorderRadius.circular(ErTokens.radiusMd),
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text('外观', style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant)),
                const SizedBox(height: 8),
                const ThemeModeSwitch(),
                const SizedBox(height: 10),
                Row(
                  children: [
                    CircleAvatar(
                      radius: 14,
                      backgroundColor: scheme.primaryContainer,
                      child: Icon(Icons.person_rounded, size: 16, color: scheme.onPrimaryContainer),
                    ),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        '本机 ' + ownDevice.prettyCode,
                        style: text.labelMedium,
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                  ],
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _NavButton extends StatefulWidget {
  const _NavButton({required this.icon, required this.label, required this.selected, required this.onTap});

  final IconData icon;
  final String label;
  final bool selected;
  final VoidCallback onTap;

  @override
  State<_NavButton> createState() => _NavButtonState();
}

class _NavButtonState extends State<_NavButton> {
  bool _hover = false;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final selected = widget.selected;
    final bg = selected
        ? scheme.primaryContainer
        : _hover
            ? scheme.surfaceContainerHigh
            : Colors.transparent;
    final fg = selected ? scheme.onPrimaryContainer : scheme.onSurfaceVariant;

    return MouseRegion(
      onEnter: (_) => setState(() => _hover = true),
      onExit: (_) => setState(() => _hover = false),
      child: InkWell(
        onTap: widget.onTap,
        borderRadius: BorderRadius.circular(ErTokens.radiusMd),
        child: AnimatedContainer(
          duration: const Duration(milliseconds: 120),
          height: 46,
          padding: const EdgeInsets.symmetric(horizontal: 12),
          decoration: BoxDecoration(color: bg, borderRadius: BorderRadius.circular(ErTokens.radiusMd)),
          child: Row(
            children: [
              Icon(widget.icon, size: 21, color: selected ? scheme.primary : fg),
              const SizedBox(width: 12),
              Expanded(
                child: Text(
                  widget.label,
                  style: Theme.of(context).textTheme.labelLarge?.copyWith(
                        color: selected ? scheme.onPrimaryContainer : fg,
                        fontWeight: selected ? FontWeight.w600 : FontWeight.w500,
                      ),
                ),
              ),
              if (selected)
                Container(width: 6, height: 6, decoration: BoxDecoration(color: scheme.primary, shape: BoxShape.circle)),
            ],
          ),
        ),
      ),
    );
  }
}

/// 三态外观切换：跟随系统 / 浅色 / 深色。
class ThemeModeSwitch extends StatelessWidget {
  const ThemeModeSwitch({super.key, this.compact = false});

  final bool compact;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    return ValueListenableBuilder<ThemeMode>(
      valueListenable: erThemeMode,
      builder: (context, mode, _) {
        Widget btn(ThemeMode value, IconData icon, String label) {
          final on = mode == value;
          return Tooltip(
            message: label,
            child: InkWell(
              borderRadius: BorderRadius.circular(ErTokens.radiusSm),
              onTap: () => erThemeMode.value = value,
              child: Container(
                height: 32,
                padding: EdgeInsets.symmetric(horizontal: compact ? 8 : 9),
                decoration: BoxDecoration(
                  color: on ? scheme.surfaceContainerLowest : Colors.transparent,
                  borderRadius: BorderRadius.circular(ErTokens.radiusSm),
                  border: Border.all(color: on ? scheme.outlineVariant : Colors.transparent),
                ),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Icon(icon, size: 16, color: on ? scheme.primary : scheme.onSurfaceVariant),
                    if (!compact) ...[
                      const SizedBox(width: 5),
                      Text(
                        label,
                        style: text.labelSmall?.copyWith(
                          color: on ? scheme.onSurface : scheme.onSurfaceVariant,
                          fontWeight: on ? FontWeight.w600 : FontWeight.w500,
                        ),
                      ),
                    ],
                  ],
                ),
              ),
            ),
          );
        }

        return Container(
          padding: const EdgeInsets.all(3),
          decoration: BoxDecoration(
            color: scheme.surfaceContainerHighest.withValues(alpha: 0.7),
            borderRadius: BorderRadius.circular(ErTokens.radiusSm + 2),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              btn(ThemeMode.system, Icons.brightness_auto_rounded, '系统'),
              btn(ThemeMode.light, Icons.light_mode_rounded, '浅色'),
              btn(ThemeMode.dark, Icons.dark_mode_rounded, '深色'),
            ],
          ),
        );
      },
    );
  }
}

/// 移动端顶栏用的单按钮外观切换（点击循环）。
class ThemeModeButton extends StatelessWidget {
  const ThemeModeButton({super.key});

  @override
  Widget build(BuildContext context) {
    return ValueListenableBuilder<ThemeMode>(
      valueListenable: erThemeMode,
      builder: (context, mode, _) {
        final icon = switch (mode) {
          ThemeMode.light => Icons.light_mode_rounded,
          ThemeMode.dark => Icons.dark_mode_rounded,
          ThemeMode.system => Icons.brightness_auto_rounded,
        };
        final label = switch (mode) {
          ThemeMode.light => '浅色',
          ThemeMode.dark => '深色',
          ThemeMode.system => '跟随系统',
        };
        return TextButton.icon(
          onPressed: () => erThemeMode.value = switch (mode) {
            ThemeMode.system => ThemeMode.light,
            ThemeMode.light => ThemeMode.dark,
            ThemeMode.dark => ThemeMode.system,
          },
          icon: Icon(icon, size: 18),
          label: Text(label),
        );
      },
    );
  }
}

/// 页面头：标题 + 副标题 + 右侧操作区，桌面/移动统一。
class PageHeader extends StatelessWidget {
  const PageHeader({
    super.key,
    required this.title,
    this.subtitle,
    this.actions = const <Widget>[],
  });

  final String title;
  final String? subtitle;
  final List<Widget> actions;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    final wide = MediaQuery.sizeOf(context).width >= ErTokens.mobileBreakpoint;

    final trailing = <Widget>[
      ...actions,
      if (!wide) const ThemeModeButton(),
    ];

    return Padding(
      padding: EdgeInsets.fromLTRB(wide ? 32 : 16, wide ? 28 : 12, wide ? 32 : 16, wide ? 12 : 8),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(title, style: text.headlineSmall),
                if (subtitle != null) ...[
                  const SizedBox(height: 4),
                  Text(subtitle!, style: text.bodySmall?.copyWith(color: scheme.onSurfaceVariant)),
                ],
              ],
            ),
          ),
          if (trailing.isNotEmpty) ...[
            const SizedBox(width: 12),
            Wrap(spacing: 8, runSpacing: 8, alignment: WrapAlignment.end, children: trailing),
          ],
        ],
      ),
    );
  }
}
