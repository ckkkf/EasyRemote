#!/usr/bin/env node
// ER-902：对 git 跟踪文件做高置信 secret 扫描（私钥块/云厂商 token 前缀）。
// 只报高置信模式，不做熵扫描，避免把测试口令/示例 UUID 当密钥。
import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';

const patterns = [
  ['private-key', /-----BEGIN (?:RSA |OPENSSH |EC |DSA )?PRIVATE KEY-----/],
  ['aws-access-key', /\bAKIA[0-9A-Z]{16}\b/],
  ['github-token', /\bghp_[A-Za-z0-9]{36}\b/],
  ['slack-token', /\bxox[baprs]-[A-Za-z0-9-]{10,}\b/],
];
const files = execFileSync('git', ['ls-files', '-z'], { encoding: 'buffer' })
  .toString('utf8')
  .split('\0')
  .filter(Boolean)
  .filter((f) => !f.startsWith('docs/impl/evidence/'));
let hits = 0;
for (const file of files) {
  let text;
  try {
    text = readFileSync(file, 'utf8');
  } catch {
    continue;
  }
  for (const [name, re] of patterns) {
    if (re.test(text)) {
      console.error('SECRET? ' + name + ': ' + file);
      hits++;
    }
  }
}
if (hits > 0) {
  console.error('secret scan failed: ' + hits + ' hit(s)');
  process.exit(1);
}
console.log('secret scan OK (' + files.length + ' files)');
