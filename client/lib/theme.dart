import 'package:flutter/material.dart';

/// 品牌主色（与 design/seed.json、design/tokens.json 一致）
const seedColor = Color(0xFF1E4BDB);

/// 全局外观模式：跟随系统 / 浅色 / 深色。设置页与侧边栏共用。
final ValueNotifier<ThemeMode> erThemeMode = ValueNotifier<ThemeMode>(ThemeMode.system);

/// 设计令牌（取自 design/tokens.json，避免运行时依赖生成文件）。
class ErTokens {
  static const brand = seedColor;

  static const radiusXs = 4.0;
  static const radiusSm = 8.0;
  static const radiusMd = 12.0;
  static const radiusLg = 16.0;
  static const radiusXl = 20.0;
  static const radiusPill = 999.0;

  static const gapXs = 4.0;
  static const gapSm = 8.0;
  static const gap = 16.0;
  static const gapLg = 24.0;
  static const gapXl = 32.0;

  /// 侧边栏宽度（带文字标签）
  static const railWidth = 232.0;
  static const railWidthCollapsed = 76.0;

  /// 宽度小于该值时切换为移动端布局（底部导航）。
  static const mobileBreakpoint = 900.0;

  static const mono = TextStyle(
    fontFamily: 'monospace',
    fontFamilyFallback: ['SF Mono', 'Menlo', 'Consolas'],
    fontSize: 12,
    fontWeight: FontWeight.w600,
    letterSpacing: 2,
  );
}

const _light = {
  'primary': Color(0xFF1E4BDB),
  'onPrimary': Color(0xFFFFFFFF),
  'primaryContainer': Color(0xFFD5E3FF),
  'onPrimaryContainer': Color(0xFF020469),
  'secondary': Color(0xFF495D89),
  'onSecondary': Color(0xFFFFFFFF),
  'secondaryContainer': Color(0xFFD5E3FE),
  'onSecondaryContainer': Color(0xFF0E1D43),
  'error': Color(0xFFAA2422),
  'onError': Color(0xFFFFFFFF),
  'errorContainer': Color(0xFFFFD8D3),
  'onErrorContainer': Color(0xFF430103),
  'surface': Color(0xFFFBFCFF),
  'onSurface': Color(0xFF1A1C1E),
  'onSurfaceVariant': Color(0xFF474B56),
  'outline': Color(0xFF777C87),
  'outlineVariant': Color(0xFFC6CCD8),
  'surfaceContainerLowest': Color(0xFFFFFFFF),
  'surfaceContainerLow': Color(0xFFF4F6FB),
  'surfaceContainer': Color(0xFFEFF1F5),
  'surfaceContainerHigh': Color(0xFFE9EBEF),
  'surfaceContainerHighest': Color(0xFFE0E2E7),
  'inverseSurface': Color(0xFF2F3034),
  'onInverseSurface': Color(0xFFEFF1F5),
  'inversePrimary': Color(0xFFADC6FC),
};

const _dark = {
  'primary': Color(0xFFADC6FC),
  'onPrimary': Color(0xFF07207F),
  'primaryContainer': Color(0xFF1B3C9B),
  'onPrimaryContainer': Color(0xFFDEE8FE),
  'secondary': Color(0xFFAFC6F8),
  'onSecondary': Color(0xFF1D2E56),
  'secondaryContainer': Color(0xFF33456F),
  'onSecondaryContainer': Color(0xFFDEE8FE),
  'error': Color(0xFFFDB0A7),
  'onError': Color(0xFF610206),
  'errorContainer': Color(0xFF88030C),
  'onErrorContainer': Color(0xFFFEE0DC),
  'surface': Color(0xFF16181A),
  'onSurface': Color(0xFFE6E8EC),
  'onSurfaceVariant': Color(0xFFC6CCD8),
  'outline': Color(0xFF8B919C),
  'outlineVariant': Color(0xFF424751),
  'surfaceContainerLowest': Color(0xFF0D0E11),
  'surfaceContainerLow': Color(0xFF1A1C1E),
  'surfaceContainer': Color(0xFF202225),
  'surfaceContainerHigh': Color(0xFF282A2D),
  'surfaceContainerHighest': Color(0xFF333538),
  'inverseSurface': Color(0xFFE0E2E7),
  'onInverseSurface': Color(0xFF2B2C2F),
  'inversePrimary': Color(0xFF3056B7),
};

ColorScheme _scheme(Brightness brightness) {
  final c = brightness == Brightness.light ? _light : _dark;
  final base = ColorScheme.fromSeed(
    seedColor: seedColor,
    brightness: brightness,
    primary: c['primary'],
  );
  return base.copyWith(
    onPrimary: c['onPrimary'],
    primaryContainer: c['primaryContainer'],
    onPrimaryContainer: c['onPrimaryContainer'],
    secondary: c['secondary'],
    onSecondary: c['onSecondary'],
    secondaryContainer: c['secondaryContainer'],
    onSecondaryContainer: c['onSecondaryContainer'],
    error: c['error'],
    onError: c['onError'],
    errorContainer: c['errorContainer'],
    onErrorContainer: c['onErrorContainer'],
    surface: c['surface'],
    onSurface: c['onSurface'],
    onSurfaceVariant: c['onSurfaceVariant'],
    outline: c['outline'],
    outlineVariant: c['outlineVariant'],
    surfaceContainerLowest: c['surfaceContainerLowest'],
    surfaceContainerLow: c['surfaceContainerLow'],
    surfaceContainer: c['surfaceContainer'],
    surfaceContainerHigh: c['surfaceContainerHigh'],
    surfaceContainerHighest: c['surfaceContainerHighest'],
    inverseSurface: c['inverseSurface'],
    onInverseSurface: c['onInverseSurface'],
    inversePrimary: c['inversePrimary'],
  );
}

TextTheme _text(TextTheme base) => base.copyWith(
      displaySmall: base.displaySmall?.copyWith(fontSize: 36, height: 1.22, fontWeight: FontWeight.w600, letterSpacing: -0.6),
      headlineMedium: base.headlineMedium?.copyWith(fontSize: 28, height: 1.28, fontWeight: FontWeight.w600, letterSpacing: -0.4),
      headlineSmall: base.headlineSmall?.copyWith(fontSize: 22, height: 1.3, fontWeight: FontWeight.w600, letterSpacing: -0.3),
      titleLarge: base.titleLarge?.copyWith(fontSize: 17, height: 1.4, fontWeight: FontWeight.w600, letterSpacing: -0.1),
      titleMedium: base.titleMedium?.copyWith(fontSize: 15, height: 1.4, fontWeight: FontWeight.w600),
      titleSmall: base.titleSmall?.copyWith(fontSize: 13, height: 1.4, fontWeight: FontWeight.w600),
      bodyLarge: base.bodyLarge?.copyWith(fontSize: 15, height: 1.55),
      bodyMedium: base.bodyMedium?.copyWith(fontSize: 14, height: 1.55),
      bodySmall: base.bodySmall?.copyWith(fontSize: 12.5, height: 1.5),
      labelLarge: base.labelLarge?.copyWith(fontSize: 13.5, fontWeight: FontWeight.w600),
      labelMedium: base.labelMedium?.copyWith(fontSize: 12, fontWeight: FontWeight.w500, letterSpacing: 0.2),
      labelSmall: base.labelSmall?.copyWith(fontSize: 11, fontWeight: FontWeight.w500, letterSpacing: 0.3),
    );

ThemeData buildTheme(Brightness brightness) {
  final scheme = _scheme(brightness);
  final base = ThemeData(useMaterial3: true, colorScheme: scheme);
  final text = _text(base.textTheme);

  OutlineInputBorder border(Color color, [double width = 1]) => OutlineInputBorder(
        borderRadius: BorderRadius.circular(ErTokens.radiusMd),
        borderSide: BorderSide(color: color, width: width),
      );

  return base.copyWith(
    textTheme: text,
    scaffoldBackgroundColor: scheme.surface,
    dividerTheme: DividerThemeData(color: scheme.outlineVariant, thickness: 1, space: 1),
    cardTheme: CardThemeData(
      elevation: 0,
      color: scheme.surfaceContainerLow,
      surfaceTintColor: Colors.transparent,
      margin: EdgeInsets.zero,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(ErTokens.radiusLg),
        side: BorderSide(color: scheme.outlineVariant.withValues(alpha: 0.7)),
      ),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        minimumSize: const Size(0, 44),
        padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 12),
        shape: const StadiumBorder(),
        textStyle: text.labelLarge,
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(
        minimumSize: const Size(0, 44),
        padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 12),
        shape: const StadiumBorder(),
        side: BorderSide(color: scheme.outlineVariant),
        textStyle: text.labelLarge,
      ),
    ),
    textButtonTheme: TextButtonThemeData(
      style: TextButton.styleFrom(
        minimumSize: const Size(0, 40),
        shape: const StadiumBorder(),
        textStyle: text.labelLarge,
      ),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: scheme.surfaceContainerLowest,
      contentPadding: const EdgeInsets.symmetric(horizontal: 14, vertical: 14),
      border: border(scheme.outlineVariant),
      enabledBorder: border(scheme.outlineVariant),
      focusedBorder: border(scheme.primary, 1.6),
      hintStyle: text.bodyMedium?.copyWith(color: scheme.onSurfaceVariant.withValues(alpha: 0.7)),
      labelStyle: text.bodyMedium?.copyWith(color: scheme.onSurfaceVariant),
    ),
    chipTheme: ChipThemeData(
      backgroundColor: scheme.surfaceContainer,
      side: BorderSide(color: scheme.outlineVariant.withValues(alpha: 0.6)),
      shape: const StadiumBorder(),
      labelStyle: text.labelMedium,
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
    ),
    listTileTheme: ListTileThemeData(
      contentPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 4),
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(ErTokens.radiusMd)),
    ),
    switchTheme: SwitchThemeData(
      thumbColor: WidgetStateProperty.resolveWith((s) => s.contains(WidgetState.selected) ? scheme.onPrimary : scheme.outline),
      trackColor: WidgetStateProperty.resolveWith((s) => s.contains(WidgetState.selected) ? scheme.primary : scheme.surfaceContainerHighest),
    ),
    navigationBarTheme: NavigationBarThemeData(
      height: 68,
      backgroundColor: scheme.surfaceContainerLow,
      indicatorColor: scheme.primaryContainer,
      labelBehavior: NavigationDestinationLabelBehavior.alwaysShow,
      labelTextStyle: WidgetStateProperty.resolveWith(
        (s) => text.labelSmall?.copyWith(
          fontSize: 12,
          fontWeight: s.contains(WidgetState.selected) ? FontWeight.w600 : FontWeight.w500,
          color: s.contains(WidgetState.selected) ? scheme.onPrimaryContainer : scheme.onSurfaceVariant,
        ),
      ),
    ),
    snackBarTheme: SnackBarThemeData(
      behavior: SnackBarBehavior.floating,
      backgroundColor: scheme.inverseSurface,
      contentTextStyle: text.bodyMedium?.copyWith(color: scheme.onInverseSurface),
    ),
    tooltipTheme: TooltipThemeData(
      textStyle: text.bodySmall?.copyWith(color: scheme.onInverseSurface),
      decoration: BoxDecoration(color: scheme.inverseSurface, borderRadius: BorderRadius.circular(ErTokens.radiusSm)),
    ),
  );
}
