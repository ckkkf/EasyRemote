// 从 design/tokens.json 生成两端主题文件
// 用法: node scripts/gen-theme.mjs
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';

const t = JSON.parse(readFileSync('design/tokens.json', 'utf8'));
const { light, dark } = t.color;
mkdirSync('design/generated', { recursive: true });

// ---------- Flutter ColorScheme ----------
const toDart = (k) => {
  const cap = k.charAt(0).toUpperCase() + k.slice(1);
  return '    ' + k + ": Color(0xFF" + light[k].replace('#','').toUpperCase() + '),';
};
const dartLight = Object.keys(light).filter(k => k !== 'brand' && k !== 'onBrand' && k !== 'brandHover' && k !== 'brandPressed')
  .map(toDart).join('\n');
const dartDark = Object.keys(dark).filter(k => k !== 'brand' && k !== 'onBrand' && k !== 'brandHover' && k !== 'brandPressed')
  .map(k => '    ' + k + ": Color(0xFF" + dark[k].replace('#','').toUpperCase() + '),').join('\n');

const dart = `// 自动生成，请勿手改。源: design/tokens.json
// 重新生成: node scripts/gen-palette.mjs && node scripts/gen-theme.mjs
import 'package:flutter/material.dart';

abstract final class ErTokens {
  // ---- 品牌 ----
  static const brandLight = Color(0xFF${light.brand.replace('#','').toUpperCase()});
  static const brandDark  = Color(0xFF${dark.brand.replace('#','').toUpperCase()});

  // ---- 间距 ----
  ${Object.entries(t.spacing).map(([k,v]) => 'static const double space' + k + ' = ' + v + ';').join('\n  ')}

  // ---- 圆角 ----
  ${Object.entries(t.radius).filter(([k])=>k!=='pill').map(([k,v]) => 'static const double radius' + k.charAt(0).toUpperCase() + k.slice(1) + ' = ' + v + ';').join('\n  ')}

  static ThemeData lightTheme() => ThemeData(
    useMaterial3: true,
    colorScheme: const ColorScheme(
${dartLight}
    ),
  );

  static ThemeData darkTheme() => ThemeData(
    useMaterial3: true,
    colorScheme: const ColorScheme(
${dartDark}
    ),
  );
}
`;
writeFileSync('design/generated/tokens.dart', dart);

// ---------- Tailwind tokens ----------
const twColors = (obj) => Object.fromEntries(Object.entries(obj).map(([k,v]) => [k, v]));
const cjs = `// 自动生成，请勿手改。源: design/tokens.json
module.exports = {
  colors: {
    light: ${JSON.stringify(twColors(light), null, 6).replace(/\n/g, '\n    ')},
    dark: ${JSON.stringify(twColors(dark), null, 6).replace(/\n/g, '\n    ')},
  },
  spacing: ${JSON.stringify(t.spacing)},
  borderRadius: ${JSON.stringify(t.radius)},
  fontFamily: ${JSON.stringify(t.typography.fontFamily)},
};
`;
writeFileSync('design/generated/tailwind.tokens.cjs', cjs);

// ---------- CSS 变量（原型 / 后台直接用） ----------
const kebab = s => s.replace(/([a-z0-9])([A-Z])/g, '$1-$2').toLowerCase();
const cssVars = (obj, sel) => sel + ' {\n' + Object.entries(obj).map(([k,v]) => '  --er-' + kebab(k) + ': ' + v + ';').join('\n') + '\n}';
const css = `/* 自动生成，请勿手改。源: design/tokens.json */
:root {
  --er-font-sans: ${t.typography.fontFamily.sans};
  --er-font-mono: ${t.typography.fontFamily.mono};
${Object.entries(t.radius).map(([k,v]) => '  --er-radius-' + k + ': ' + v + 'px;').join('\n')}
${Object.entries(t.spacing).map(([k,v]) => '  --er-space-' + k + ': ' + v + 'px;').join('\n')}
}
${cssVars(light, ':root, [data-theme="light"]')}
${cssVars(dark, '[data-theme="dark"]')}
`;
writeFileSync('design/generated/tokens.css', css);

console.log('generated: tokens.dart / tailwind.tokens.cjs / tokens.css');
