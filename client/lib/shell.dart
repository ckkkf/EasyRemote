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
import 'prefs.dart';
import 'theme.dart';
import 'version_gen.dart';

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

// 导航顺序 = 页面下标。设置是**导航组里的最后一项**（紧跟文件传输），
// 不是整个侧边栏的最下方——外观/本机那类面板永远在导航组下面。
const _mainNavItems = <_ErNavItem>[
  _ErNavItem(Icons.devices_rounded, '我的设备', '已绑定设备与本机设备码'),
  _ErNavItem(Icons.pin_rounded, '连接设备', '用 9 位设备码请求远程控制'),
  _ErNavItem(Icons.monitor_heart_rounded, '网络诊断', '链路质量与可执行建议'),
  _ErNavItem(Icons.science_rounded, '会话自检', '走真实服务与真实采集'),
  _ErNavItem(Icons.folder_rounded, '文件传输', '上传、下载与删除共享文件'),
];

const _settingsNav = _ErNavItem(Icons.tune_rounded, '设置', '外观、画质与安全');

/// 侧边栏/底部导航的完整顺序：业务导航 + 设置（设置永远是这一组的最后一项）。
const _navItems = <_ErNavItem>[..._mainNavItems, _settingsNav];

/// 设置页在列表中的下标（永远最后）。
final _settingsIndex = _mainNavItems.length;

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

  // 顺序必须与 _mainNavItems + _settingsNav 一致。
  List<Widget> get _pages => <Widget>[
    DeviceListPage(
      client: widget.client,
      ownDevice: widget.device,
      onConnect: _goToConnect,
      onSessionStarted: _openSession,
    ),
    ConnectPage(
      client: widget.client,
      initialCode: _connectPrefill,
      recents: _recents,
      onCodeUsed: _rememberCode,
      onSessionStarted: _openSession,
    ),
    DiagnosticsPage(client: widget.client, config: _config),
    const SelfTestPage(),
    FilesPage(client: widget.client, config: _config),
    SettingsPage(device: widget.device, config: _config, onLogout: _logout),
  ];

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final wide = MediaQuery.sizeOf(context).width >= ErTokens.mobileBreakpoint;

    final pages = _pages;
    assert(
      _navItems.length == pages.length && _settingsIndex == _navItems.length - 1,
      '导航项必须与页面一一对应，且设置永远是导航组最后一项',
    );

    if (!wide) {
      return Scaffold(
        // IndexedStack 而不是 pages[index]：
        // 换页时旧页面不再被 dispose，回来时滚动位置/输入/被控连接都还在，
        // 也不会先闪一次加载态。被控页尤其重要——它一 dispose 就会 stop 被控。
        body: SafeArea(
          bottom: false,
          child: IndexedStack(
            index: _index,
            sizing: StackFit.expand,
            children: pages,
          ),
        ),
        bottomNavigationBar: NavigationBar(
          selectedIndex: _index,
          onDestinationSelected: (i) => setState(() => _index = i),
          destinations: [
            for (final item in _navItems)
              NavigationDestination(
                icon: Icon(item.icon, size: 22),
                label: item.label,
              ),
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
          VerticalDivider(
            width: 1,
            color: scheme.outlineVariant.withValues(alpha: 0.6),
          ),
          Expanded(
            child: IndexedStack(
              index: _index,
              sizing: StackFit.expand,
              children: pages,
            ),
          ),
        ],
      ),
    );
  }
}

class _Sidebar extends StatelessWidget {
  const _Sidebar({
    required this.index,
    required this.onSelect,
    required this.ownDevice,
  });

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
                  child: Text(
                    'ER',
                    style: text.labelLarge?.copyWith(
                      color: Colors.white,
                      letterSpacing: 0.5,
                    ),
                  ),
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text('EasyRemote', style: text.titleMedium),
                      Text(
                        // 版本号来自 scripts/check-version.mjs 生成的常量，
                        // 别手写：之前这里硬编码 v1.0.0，跟 pubspec 的真实版本完全脱节。
                        'v' + erAppVersion,
                        style: text.labelSmall?.copyWith(
                          color: scheme.onSurfaceVariant,
                        ),
                      ),
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
                Text(
                  '外观',
                  style: text.labelSmall?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
                ),
                const SizedBox(height: 8),
                // 侧边栏只有 186px 可用：紧凑模式（仅图标 + Tooltip）才放得下三档。
                const ThemeModeSwitch(compact: true),
                const SizedBox(height: 10),
                Row(
                  children: [
                    CircleAvatar(
                      radius: 14,
                      backgroundColor: scheme.primaryContainer,
                      child: Icon(
                        Icons.person_rounded,
                        size: 16,
                        color: scheme.onPrimaryContainer,
                      ),
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

class _NavButton extends StatelessWidget {
  const _NavButton({
    required this.icon,
    required this.label,
    required this.selected,
    required this.onTap,
  });

  final IconData icon;
  final String label;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final fg = selected ? scheme.onPrimaryContainer : scheme.onSurfaceVariant;

    return Material(
      // 未选中用「侧边栏同底色」而不是 Colors.transparent：
      // Material 的底色过渡是按 RGBA 分量线性插值的，插到 (0,0,0,0) 会在中途变暗，
      // 鼠标移出时就会闪一下灰黑——那是「离开还在闪」的真正原因。
      color: selected ? scheme.primaryContainer : scheme.surfaceContainerLow,
      animationDuration: const Duration(milliseconds: 120),
      borderRadius: BorderRadius.circular(ErTokens.radiusMd),
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: onTap,
        // 悬停/按下交给 InkWell 原生处理：它给一块实色做透明度动画，
        // 不会经过半透明中间色，也不会和底色叠成两层。
        hoverColor: scheme.surfaceContainerHigh,
        child: SizedBox(
          height: 46,
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 12),
            child: Row(
              children: [
                Icon(icon, size: 21, color: selected ? scheme.primary : fg),
                const SizedBox(width: 12),
                Expanded(
                  child: Text(
                    label,
                    style: Theme.of(context).textTheme.labelLarge?.copyWith(
                      color: selected ? scheme.onPrimaryContainer : fg,
                      fontWeight: selected ? FontWeight.w600 : FontWeight.w400,
                    ),
                  ),
                ),
                if (selected)
                  Container(
                    width: 6,
                    height: 6,
                    decoration: BoxDecoration(
                      color: scheme.primary,
                      shape: BoxShape.circle,
                    ),
                  ),
              ],
            ),
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
              onTap: () => setThemeMode(value),
              child: Container(
                height: 32,
                padding: EdgeInsets.symmetric(horizontal: compact ? 8 : 9),
                decoration: BoxDecoration(
                  color: on
                      ? scheme.surfaceContainerLowest
                      : Colors.transparent,
                  borderRadius: BorderRadius.circular(ErTokens.radiusSm),
                  border: Border.all(
                    color: on ? scheme.outlineVariant : Colors.transparent,
                  ),
                ),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Icon(
                      icon,
                      size: 16,
                      color: on ? scheme.primary : scheme.onSurfaceVariant,
                    ),
                    if (!compact) ...[
                      const SizedBox(width: 5),
                      Text(
                        label,
                        style: text.labelSmall?.copyWith(
                          color: on
                              ? scheme.onSurface
                              : scheme.onSurfaceVariant,
                          fontWeight: on ? FontWeight.w600 : FontWeight.w400,
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
          onPressed: () => setThemeMode(switch (mode) {
            ThemeMode.system => ThemeMode.light,
            ThemeMode.light => ThemeMode.dark,
            ThemeMode.dark => ThemeMode.system,
          }),
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

    final trailing = <Widget>[...actions, if (!wide) const ThemeModeButton()];

    final canPop = Navigator.of(context).canPop();

    return Padding(
      padding: EdgeInsets.fromLTRB(
        wide ? 32 : 16,
        wide ? 28 : 12,
        wide ? 32 : 16,
        wide ? 12 : 8,
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // 二级页面（诊断等）必须能退回上一层：之前只画标题，用户走到这里就没有回头路。
          if (canPop)
            Padding(
              padding: const EdgeInsets.only(right: 8, top: 2),
              child: IconButton(
                tooltip: "返回",
                onPressed: () => Navigator.of(context).maybePop(),
                icon: const Icon(Icons.arrow_back_rounded, size: 20),
              ),
            ),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(title, style: text.headlineSmall),
                if (subtitle != null) ...[
                  const SizedBox(height: 4),
                  Text(
                    subtitle!,
                    style: text.bodySmall?.copyWith(
                      color: scheme.onSurfaceVariant,
                    ),
                  ),
                ],
              ],
            ),
          ),
          if (trailing.isNotEmpty) ...[
            const SizedBox(width: 12),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              alignment: WrapAlignment.end,
              children: trailing,
            ),
          ],
        ],
      ),
    );
  }
}
