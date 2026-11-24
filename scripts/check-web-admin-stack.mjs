#!/usr/bin/env node
// ADM-UI-01a 技术栈断言：Tailwind 令牌接线、shadcn primitive 源码化、构建产物。
//
// 用法:
//   node scripts/check-web-admin-stack.mjs           静态断言（不需要 dist）
//   node scripts/check-web-admin-stack.mjs --build   追加构建产物断言（先跑 npm run build）
import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const ADMIN = join(ROOT, 'web-admin');
const TOKENS_CJS = 'design/generated/tailwind.tokens.cjs';
const TOKENS_CSS = 'design/generated/tokens.css';
const PRIMITIVES = ['button', 'input', 'card', 'badge', 'label', 'alert'];

const require = createRequire(import.meta.url);
const tokens = require(join(ROOT, TOKENS_CJS));
const brand = String(tokens.colors.light.brand).replace('#', '').toLowerCase();

const failures = [];
const checks = [];

function check(name, fn) {
  try {
    const note = fn() || '';
    checks.push({ name, ok: true, note });
    console.log('PASS ' + name + (note ? ' · ' + note : ''));
  } catch (err) {
    failures.push(name);
    checks.push({ name, ok: false, note: err.message });
    console.log('FAIL ' + name + ' · ' + err.message);
  }
}

function readAdmin(rel) {
  const abs = join(ADMIN, rel);
  if (!existsSync(abs)) throw new Error('缺少文件 ' + rel);
  return readFileSync(abs, 'utf8');
}

function expectContains(text, needle, where) {
  if (!text.includes(needle)) throw new Error(where + ' 未引用 ' + needle);
}

check('TestTailwindWiredToGeneratedTokens', () => {
  const config = readAdmin('tailwind.config.ts');
  expectContains(config, TOKENS_CJS, 'tailwind.config.ts');
  if (!/preflight\s*:\s*false/.test(config)) {
    throw new Error('tailwind.config.ts 必须设置 corePlugins.preflight = false（现有 styles.css 仍生效）');
  }

  const globals = readAdmin('src/styles/globals.css');
  expectContains(globals, TOKENS_CSS, 'src/styles/globals.css');
  const hardcoded = globals.match(/#1e4bdb/gi);
  if (hardcoded) {
    throw new Error('globals.css 出现硬编码品牌色 ' + hardcoded[0] + '，必须引用生成令牌');
  }

  const postcss = readAdmin('postcss.config.js');
  expectContains(postcss, 'tailwindcss', 'postcss.config.js');
  expectContains(postcss, 'autoprefixer', 'postcss.config.js');
  return 'token 单一来源 + preflight 关闭';
});

check('TestShadcnPrimitivesAreSource', () => {
  const uiDir = join(ADMIN, 'src/components/ui');
  for (const name of PRIMITIVES) {
    const file = join(uiDir, name + '.tsx');
    if (!existsSync(file)) throw new Error('缺少 primitive ' + name + '.tsx');
    const src = readFileSync(file, 'utf8');
    if (!src.includes('cn(')) throw new Error(name + '.tsx 未使用 cn() 合并类名');
  }

  // 组件目录内不得出现网络调用：primitives 只负责表现层。
  for (const name of readdirSync(uiDir)) {
    if (!name.endsWith('.tsx') && !name.endsWith('.ts')) continue;
    const src = readFileSync(join(uiDir, name), 'utf8');
    if (/\bfetch\s*\(/.test(src)) throw new Error(name + ' 出现 fetch( 调用');
    if (/from ['"]axios['"]/.test(src)) throw new Error(name + ' 引入 axios');
  }

  // 禁止用 shadcn 运行时黑盒包代替仓库源码。
  const pkg = JSON.parse(readAdmin('package.json'));
  for (const section of ['dependencies', 'devDependencies']) {
    for (const dep of Object.keys(pkg[section] || {})) {
      if (dep === 'shadcn' || dep === '@shadcn/ui') {
        throw new Error('package.json 不得依赖 ' + dep);
      }
    }
  }
  return PRIMITIVES.length + ' 个源码 primitive';
});

// ADM-UI-03：生产 UI 不得展示开发账号/开发口令/关闭 MFA 的做法。
// 源码与构建产物都要查——只查源码会漏掉被内联进 bundle 的常量。
check('TestLoginHasNoDevCredentialsInBundle', () => {
  const banned = ['dev-pass', 'ER_ADMIN_TOTP_OFF'];
  const srcDir = join(ADMIN, 'src');
  const walk = (dir) => {
    for (const name of readdirSync(dir, { withFileTypes: true })) {
      const abs = join(dir, name.name);
      if (name.isDirectory()) walk(abs);
      else if (/\.(ts|tsx|css)$/.test(name.name)) {
        const text = readFileSync(abs, 'utf8');
        for (const needle of banned) {
          if (text.includes(needle)) throw new Error('源码 ' + abs.slice(ROOT.length + 1) + ' 含 ' + needle);
        }
      }
    }
  };
  walk(srcDir);

  const assets = join(ADMIN, 'dist/assets');
  let scanned = 'src';
  if (process.argv.includes('--build') && existsSync(assets)) {
    const js = readdirSync(assets).filter((f) => f.endsWith('.js'));
    for (const f of js) {
      const text = readFileSync(join(assets, f), 'utf8');
      for (const needle of banned) {
        if (text.includes(needle)) throw new Error('构建产物 ' + f + ' 含 ' + needle);
      }
    }
    scanned = 'src + dist/' + js.join(',');
  }
  return '无开发口令泄漏（' + scanned + '）';
});

if (process.argv.includes('--build')) {
  check('TestTailwindBuildEmitsUtility', () => {
    const assets = join(ADMIN, 'dist/assets');
    if (!existsSync(assets)) throw new Error('缺少 dist/assets，先运行 npm run build');
    const cssFiles = readdirSync(assets).filter((f) => f.endsWith('.css'));
    if (cssFiles.length === 0) throw new Error('dist/assets 下没有 CSS 产物');
    const css = cssFiles.map((f) => readFileSync(join(assets, f), 'utf8')).join('\n');
    const lower = css.toLowerCase();
    if (!lower.includes(brand)) {
      throw new Error('构建产物未包含生成令牌的品牌色 #' + brand);
    }
    for (const util of ['.inline-flex', '.h-9', '.w-full']) {
      if (!lower.includes(util)) throw new Error('构建产物缺少 Tailwind 工具类产物 ' + util);
    }
    return cssFiles.join(', ') + ' · 品牌色 #' + brand + ' + 工具类';
  });
} else {
  console.log('SKIP TestTailwindBuildEmitsUtility · 需要 --build（先 npm run build）');
}

console.log('');
console.log('汇总: ' + checks.filter((c) => c.ok).length + '/' + checks.length + ' 通过' + (failures.length ? '，失败 ' + failures.join(', ') : ''));
process.exit(failures.length === 0 ? 0 : 1);
