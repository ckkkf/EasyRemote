// 自动生成，请勿手改。源: design/tokens.json
// 重新生成: node scripts/gen-palette.mjs && node scripts/gen-theme.mjs
import 'package:flutter/material.dart';

abstract final class ErTokens {
  // ---- 品牌 ----
  static const brandLight = Color(0xFF1E4BDB);
  static const brandDark  = Color(0xFF94B5FD);

  // ---- 间距 ----
  static const double space0 = 0;
  static const double space1 = 2;
  static const double space2 = 4;
  static const double space3 = 6;
  static const double space4 = 8;
  static const double space5 = 12;
  static const double space6 = 16;
  static const double space7 = 20;
  static const double space8 = 24;
  static const double space9 = 32;
  static const double space10 = 40;
  static const double space11 = 56;
  static const double space12 = 72;

  // ---- 圆角 ----
  static const double radiusNone = 0;
  static const double radiusXs = 4;
  static const double radiusSm = 6;
  static const double radiusMd = 8;
  static const double radiusLg = 12;
  static const double radiusXl = 16;
  static const double radius2xl = 20;

  static ThemeData lightTheme() => ThemeData(
    useMaterial3: true,
    colorScheme: const ColorScheme(
    primary: Color(0xFF1E4BDB),
    onPrimary: Color(0xFFFFFFFF),
    primaryContainer: Color(0xFFD5E3FF),
    onPrimaryContainer: Color(0xFF020469),
    secondary: Color(0xFF495D89),
    onSecondary: Color(0xFFFFFFFF),
    secondaryContainer: Color(0xFFD5E3FE),
    onSecondaryContainer: Color(0xFF0E1D43),
    tertiary: Color(0xFF7D477D),
    onTertiary: Color(0xFFFFFFFF),
    tertiaryContainer: Color(0xFFFFD1FE),
    onTertiaryContainer: Color(0xFF370339),
    error: Color(0xFFAA2422),
    onError: Color(0xFFFFFFFF),
    errorContainer: Color(0xFFFFD8D3),
    onErrorContainer: Color(0xFF430103),
    surface: Color(0xFFFBFCFF),
    onSurface: Color(0xFF1A1C1E),
    surfaceVariant: Color(0xFFE2E8F5),
    onSurfaceVariant: Color(0xFF474B56),
    outline: Color(0xFF777C87),
    outlineVariant: Color(0xFFC6CCD8),
    surfaceContainerLowest: Color(0xFFFFFFFF),
    surfaceContainerLow: Color(0xFFF4F6FB),
    surfaceContainer: Color(0xFFEFF1F5),
    surfaceContainerHigh: Color(0xFFE9EBEF),
    surfaceContainerHighest: Color(0xFFE0E2E7),
    inverseSurface: Color(0xFF2F3034),
    inverseOnSurface: Color(0xFFEFF1F5),
    inversePrimary: Color(0xFFADC6FC),
    scrim: Color(0xFF000000),
    shadow: Color(0xFF000000),
    ok: Color(0xFF2A6239),
    onOk: Color(0xFFEDFFF0),
    okContainer: Color(0xFFC8F6D0),
    onOkContainer: Color(0xFF013715),
    warn: Color(0xFF78510B),
    warnContainer: Color(0xFFFEE4C1),
    onWarnContainer: Color(0xFF3D2701),
    ),
  );

  static ThemeData darkTheme() => ThemeData(
    useMaterial3: true,
    colorScheme: const ColorScheme(
    primary: Color(0xFFADC6FC),
    onPrimary: Color(0xFF07207F),
    primaryContainer: Color(0xFF1B3C9B),
    onPrimaryContainer: Color(0xFFDEE8FE),
    secondary: Color(0xFFAFC6F8),
    onSecondary: Color(0xFF1D2E56),
    secondaryContainer: Color(0xFF33456F),
    onSecondaryContainer: Color(0xFFDEE8FE),
    tertiary: Color(0xFFEBAFEA),
    onTertiary: Color(0xFF4A174B),
    tertiaryContainer: Color(0xFF632F64),
    onTertiaryContainer: Color(0xFFFFDBFE),
    error: Color(0xFFFDB0A7),
    onError: Color(0xFF610206),
    errorContainer: Color(0xFF88030C),
    onErrorContainer: Color(0xFFFEE0DC),
    surface: Color(0xFF16181A),
    onSurface: Color(0xFFE6E8EC),
    surfaceVariant: Color(0xFF424751),
    onSurfaceVariant: Color(0xFFC6CCD8),
    outline: Color(0xFF8B919C),
    outlineVariant: Color(0xFF424751),
    surfaceContainerLowest: Color(0xFF0D0E11),
    surfaceContainerLow: Color(0xFF1A1C1E),
    surfaceContainer: Color(0xFF202225),
    surfaceContainerHigh: Color(0xFF282A2D),
    surfaceContainerHighest: Color(0xFF333538),
    inverseSurface: Color(0xFFE0E2E7),
    inverseOnSurface: Color(0xFF2B2C2F),
    inversePrimary: Color(0xFF3056B7),
    scrim: Color(0xFF000000),
    shadow: Color(0xFF000000),
    ok: Color(0xFF92CC9D),
    onOk: Color(0xFF053114),
    okContainer: Color(0xFF1F4829),
    onOkContainer: Color(0xFFB8F4C3),
    warn: Color(0xFFF4BB68),
    warnContainer: Color(0xFF5A3B03),
    onWarnContainer: Color(0xFFFEDDB0),
    ),
  );
}
