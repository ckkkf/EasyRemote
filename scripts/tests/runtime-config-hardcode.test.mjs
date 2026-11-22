import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';

const ROOT = path.resolve(import.meta.dirname, '..', '..');
const TARGETS = ['client/lib', 'core/crates/er-client/src'];
const BANNED = /(127\.0\.0\.1|localhost|:18090|:19000|:18099|:19121)/;

function walk(dir) {
  const out = [];
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) out.push(...walk(full));
    else if (/\.(dart|rs)$/.test(entry.name) && !entry.name.endsWith('_test.dart')) out.push(full);
  }
  return out;
}

test('发布代码不得硬编码本机端点', () => {
  const offenders = [];
  for (const target of TARGETS) {
    for (const file of walk(path.join(ROOT, target))) {
      const text = fs.readFileSync(file, 'utf8');
      const lines = text.split('\n');
      lines.forEach((line, i) => {
        if (BANNED.test(line)) offenders.push(path.relative(ROOT, file) + ':' + (i + 1) + ': ' + line.trim());
      });
    }
  }
  assert.deepEqual(offenders, [], '运行时端点必须来自 RuntimeConfig，不能烧在业务代码里:\n' + offenders.join('\n'));
});
