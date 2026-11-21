// design-tokens 生成器：从 seed 色生成 Material 3 风格的完整色板
// 用法: node scripts/gen-palette.mjs
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';

// ---------- sRGB <-> OKLab ----------
const srgbToLinear = c => (c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4));
const linearToSrgb = c => (c <= 0.0031308 ? c * 12.92 : 1.055 * Math.pow(c, 1 / 2.4) - 0.055);

function hexToRgb(hex) {
  const h = hex.replace('#', '');
  return [parseInt(h.slice(0,2),16)/255, parseInt(h.slice(2,4),16)/255, parseInt(h.slice(4,6),16)/255];
}
function rgbToHex([r,g,b]) {
  const to = v => Math.max(0, Math.min(255, Math.round(v*255))).toString(16).padStart(2,'0');
  return '#' + to(r) + to(g) + to(b);
}

function rgbToOklab([r,g,b]) {
  const R = srgbToLinear(r), G = srgbToLinear(g), B = srgbToLinear(b);
  const l = 0.4122214708*R + 0.5363325363*G + 0.0514459929*B;
  const m = 0.2119034982*R + 0.6806995451*G + 0.1073969566*B;
  const s = 0.0883024619*R + 0.2817188376*G + 0.6299787005*B;
  const l_ = Math.cbrt(l), m_ = Math.cbrt(m), s_ = Math.cbrt(s);
  return [
    0.2104542553*l_ + 0.7936177850*m_ - 0.0040720468*s_,
    1.9779984951*l_ - 2.4285922050*m_ + 0.4505937099*s_,
    0.0259040371*l_ + 0.7827717662*m_ - 0.8086757660*s_,
  ];
}
function oklabToRgb([L,a,b]) {
  const l_ = L + 0.3963377774*a + 0.2158037573*b;
  const m_ = L - 0.1055613458*a - 0.0638541728*b;
  const s_ = L - 0.0894841775*a - 1.2914855480*b;
  const l = l_**3, m = m_**3, s = s_**3;
  const R = +4.0767416621*l - 3.3077115913*m + 0.2309699292*s;
  const G = -1.2684380046*l + 2.6097574011*m - 0.3413193965*s;
  const B = -0.0041960863*l - 0.7034186147*m + 1.7076147010*s;
  return [linearToSrgb(R), linearToSrgb(G), linearToSrgb(B)];
}

const inGamut = rgb => rgb.every(v => v >= -0.0005 && v <= 1.0005);

// M3 tone(0..100) -> OKLab L
function toneToL(tone) {
  const Lstar = tone;
  const Y = Lstar > 8 ? Math.pow((Lstar + 16) / 116, 3) : Lstar / 903.3;
  return Math.cbrt(Y);
}

// 在给定 hue/chroma/tone 下做 gamut 映射（逐步降 chroma）
function toneColor(hueDeg, chroma, tone) {
  const L = toneToL(tone);
  const hRad = (hueDeg * Math.PI) / 180;
  let c = chroma;
  for (let i = 0; i < 60; i++) {
    const a = c * Math.cos(hRad);
    const b = c * Math.sin(hRad);
    const rgb = oklabToRgb([L, a, b]);
    if (inGamut(rgb)) return rgbToHex(rgb);
    c *= 0.94;
  }
  return rgbToHex(oklabToRgb([L, 0, 0]));
}

const seedHex = (JSON.parse(readFileSync('design/seed.json','utf8'))).seed;
const [ , , ] = [0,0,0];
const lab = rgbToOklab(hexToRgb(seedHex));
const hue = Math.atan2(lab[2], lab[1]) * 180 / Math.PI;
const chroma = Math.min(0.16, Math.hypot(lab[1], lab[2]));

const H = { primary: hue, secondary: hue, tertiary: hue + 62, neutral: hue, neutralVariant: hue, error: 27 };
const C = { primary: chroma, secondary: 0.075, tertiary: 0.105, neutral: 0.006, neutralVariant: 0.018, error: 0.17 };

const c = (pal, tone) => toneColor(H[pal], C[pal], tone);

const light = {
  // 品牌主色：浅色主题直接用 seed 本身，保证按钮饱和度（M3 默认 tone40 会明显发闷）
  primary:            seedHex,
  onPrimary:          '#FFFFFF',
  brand:              seedHex,
  onBrand:            '#FFFFFF',
  brandHover:         c('primary', 32),
  brandPressed:       c('primary', 26),
  primaryContainer:   c('primary', 90),
  onPrimaryContainer: c('primary', 12),
  secondary:          c('secondary', 40),
  onSecondary:        c('secondary', 100),
  secondaryContainer: c('secondary', 90),
  onSecondaryContainer: c('secondary', 12),
  tertiary:           c('tertiary', 40),
  onTertiary:         c('tertiary', 100),
  tertiaryContainer:  c('tertiary', 90),
  onTertiaryContainer:c('tertiary', 12),
  error:              c('error', 40),
  onError:            c('error', 100),
  errorContainer:     c('error', 90),
  onErrorContainer:   c('error', 12),
  surface:            c('neutral', 99),
  onSurface:          c('neutral', 10),
  surfaceVariant:     c('neutralVariant', 92),
  onSurfaceVariant:   c('neutralVariant', 32),
  outline:            c('neutralVariant', 52),
  outlineVariant:     c('neutralVariant', 82),
  surfaceContainerLowest:  c('neutral', 100),
  surfaceContainerLow:     c('neutral', 97),
  surfaceContainer:        c('neutral', 95),
  surfaceContainerHigh:    c('neutral', 93),
  surfaceContainerHighest: c('neutral', 90),
  inverseSurface:     c('neutral', 20),
  inverseOnSurface:   c('neutral', 95),
  inversePrimary:     c('primary', 80),
  scrim:              '#000000',
  shadow:             '#000000',
  ok:                 toneColor(150, 0.09, 36),
  onOk:               toneColor(150, 0.06, 98),
  okContainer:        toneColor(150, 0.07, 92),
  onOkContainer:      toneColor(150, 0.09, 18),
  warn:               toneColor(75, 0.12, 38),
  warnContainer:      toneColor(75, 0.10, 92),
  onWarnContainer:    toneColor(75, 0.12, 18),
};

const dark = {
  primary:            c('primary', 80),
  brand:              c('primary', 74),
  onBrand:            c('primary', 12),
  brandHover:         c('primary', 80),
  brandPressed:       c('primary', 86),
  onPrimary:          c('primary', 20),
  primaryContainer:   c('primary', 30),
  onPrimaryContainer: c('primary', 92),
  secondary:          c('secondary', 80),
  onSecondary:        c('secondary', 20),
  secondaryContainer: c('secondary', 30),
  onSecondaryContainer: c('secondary', 92),
  tertiary:           c('tertiary', 80),
  onTertiary:         c('tertiary', 20),
  tertiaryContainer:  c('tertiary', 30),
  onTertiaryContainer:c('tertiary', 92),
  error:              c('error', 80),
  onError:            c('error', 20),
  errorContainer:     c('error', 30),
  onErrorContainer:   c('error', 92),
  surface:            c('neutral', 8),
  onSurface:          c('neutral', 92),
  surfaceVariant:     c('neutralVariant', 30),
  onSurfaceVariant:   c('neutralVariant', 82),
  outline:            c('neutralVariant', 60),
  outlineVariant:     c('neutralVariant', 30),
  surfaceContainerLowest:  c('neutral', 4),
  surfaceContainerLow:     c('neutral', 10),
  surfaceContainer:        c('neutral', 13),
  surfaceContainerHigh:    c('neutral', 17),
  surfaceContainerHighest: c('neutral', 22),
  inverseSurface:     c('neutral', 90),
  inverseOnSurface:   c('neutral', 18),
  inversePrimary:     c('primary', 40),
  scrim:              '#000000',
  shadow:             '#000000',
  ok:                 toneColor(150, 0.09, 76),
  onOk:               toneColor(150, 0.07, 16),
  okContainer:        toneColor(150, 0.07, 26),
  onOkContainer:      toneColor(150, 0.09, 90),
  warn:               toneColor(75, 0.12, 80),
  warnContainer:      toneColor(75, 0.10, 28),
  onWarnContainer:    toneColor(75, 0.12, 90),
};

const out = {
  "$schema": "easyremote/design-tokens@1",
  meta: {
    generatedBy: "scripts/gen-palette.mjs",
    note: "单一真源。改 seed 后重跑本脚本，再跑 gen-theme.mjs 生成两端主题。",
    seed: seedHex,
    seedOklch: { hue: +hue.toFixed(1), chroma: +chroma.toFixed(4) },
  },
  spacing: { 0: 0, 1: 2, 2: 4, 3: 6, 4: 8, 5: 12, 6: 16, 7: 20, 8: 24, 9: 32, 10: 40, 11: 56, 12: 72 },
  radius: { none: 0, xs: 4, sm: 6, md: 8, lg: 12, xl: 16, "2xl": 20, pill: 999 },
  typography: {
    fontFamily: { sans: "Inter, PingFang SC, Microsoft YaHei, sans-serif", mono: "SF Mono, JetBrains Mono, Cascadia Mono, Menlo, monospace" },
    scale: {
      display: { size: 40, lineHeight: 48, weight: 650, letterSpacing: -0.02 },
      headline: { size: 28, lineHeight: 36, weight: 640, letterSpacing: -0.015 },
      title: { size: 17, lineHeight: 24, weight: 620, letterSpacing: -0.01 },
      body: { size: 14, lineHeight: 22, weight: 400, letterSpacing: 0 },
      label: { size: 12, lineHeight: 16, weight: 550, letterSpacing: 0.01 },
      caption: { size: 11, lineHeight: 15, weight: 500, letterSpacing: 0.02 },
      mono: { size: 12, lineHeight: 18, weight: 550, letterSpacing: 0.04 },
    },
  },
  color: { light, dark },
};

mkdirSync('design', { recursive: true });
writeFileSync('design/tokens.json', JSON.stringify(out, null, 2) + '\n');
console.log('roles:', Object.keys(light).length, '→ design/tokens.json');
console.log('light.primary =', light.primary, '| dark.primary =', dark.primary);
console.log('light.surface =', light.surface, '| light.outline =', light.outline);
