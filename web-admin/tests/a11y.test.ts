// ADM-UI-10：可访问性的静态契约（动效、焦点、对比度）——这些一旦被改回去要立刻红。
import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const SRC = resolve(__dirname, '../src');
const read = (rel: string) => readFileSync(resolve(SRC, rel), 'utf8');

type Hsl = { h: number; s: number; l: number };

function hslToRgb({ h, s, l }: Hsl) {
  const sat = s / 100;
  const light = l / 100;
  const k = (n: number) => (n + h / 30) % 12;
  const a = sat * Math.min(light, 1 - light);
  const f = (n: number) => light - a * Math.max(-1, Math.min(k(n) - 3, Math.min(9 - k(n), 1)));
  return { r: f(0) * 255, g: f(8) * 255, b: f(4) * 255 };
}

function luminance({ r, g, b }: { r: number; g: number; b: number }) {
  const f = (v: number) => {
    const s = v / 255;
    return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
  };
  return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
}

function contrast(a: Hsl, b: Hsl) {
  const l1 = luminance(hslToRgb(a));
  const l2 = luminance(hslToRgb(b));
  const hi = Math.max(l1, l2);
  const lo = Math.min(l1, l2);
  return (hi + 0.05) / (lo + 0.05);
}

/** 从 globals.css 读一个 shadcn 主题变量（形如 `--muted-foreground: 240 3.8% 46.1%;`）。 */
function themeVar(css: string, name: string): Hsl {
  const m = css.match(new RegExp('--' + name + ':\\s*([\\d.]+)\\s+([\\d.]+)%\\s+([\\d.]+)%'));
  if (!m) throw new Error('missing theme var: ' + name);
  return { h: Number(m[1]), s: Number(m[2]), l: Number(m[3]) };
}

describe('a11y contract', () => {
  it('TestReducedMotionHonoured', () => {
    const css = read('styles/globals.css');
    expect(css).toContain('prefers-reduced-motion: reduce');
    expect(css).toContain('transition-duration: 0.01ms');
  });

  it('TestFocusVisibleStyles', () => {
    const css = read('styles.css');
    expect(css).toContain(':focus-visible');
    // 主题切到 shadcn 默认变量后，焦点环用 --primary（不再是自有 --brand）。
    expect(css).toContain('outline: 2px solid hsl(var(--primary))');
    // 文本链接也是交互目标，必须满足最小 24px 高度。
    // 所有交互元素共用焦点环规则；链接类按钮改用 shadcn 的 link variant（不再有 .link 类）。
    expect(css).toMatch(/:where\(a, button, input, select, textarea\):focus-visible/);
  });

  it('TestMutedTextContrastMeetsAA', () => {
    const css = read('styles/globals.css');
    expect(contrast(themeVar(css, 'muted-foreground'), themeVar(css, 'background'))).toBeGreaterThanOrEqual(4.5);
  });

  it('TestForegroundContrastMeetsAAA', () => {
    const css = read('styles/globals.css');
    expect(contrast(themeVar(css, 'foreground'), themeVar(css, 'background'))).toBeGreaterThanOrEqual(7);
  });
});
