// ER-011：每个端点的 response 都必须是具体 DTO（或文件字节流），三端生成物字段齐全。
import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { parseContract } from '../check-routes.mjs';

const CONTRACT = readFileSync('docs/spec/contract.yaml', 'utf8');
const endpoints = parseContract(CONTRACT);

function parseTypes(text) {
  const lines = text.split('\n');
  const types = new Map();
  let inTypes = false;
  let name = null;
  for (const raw of lines) {
    if (/^types:\s*$/.test(raw)) { inTypes = true; continue; }
    if (/^endpoints:\s*$/.test(raw)) break;
    if (!inTypes) continue;
    const m = raw.match(/^  ([A-Za-z][A-Za-z0-9]*):\s*$/);
    if (m) { name = m[1]; types.set(name, []); continue; }
    const f = raw.match(/^    ([a-z][a-z0-9_]*):\s*(\S+)\s*$/);
    if (f && name) types.get(name).push({ field: f[1], type: f[2] });
  }
  return types;
}

const types = parseTypes(CONTRACT);

test('TestContractHasNoAnyResponse', () => {
  const anys = endpoints.filter((e) => e.response === 'any' || e.response === 'unknown');
  assert.deepEqual(anys.map((e) => e.id), [], '完成态不允许 response: any');
  for (const e of endpoints) {
    assert.ok(e.response, e.id + ' 缺 response');
  }
});

test('TestEveryEndpointResponseHasConcreteDTO', () => {
  const go = readFileSync('server/internal/contract/types_gen.go', 'utf8');
  const ts = readFileSync('web-admin/src/contract.ts', 'utf8');
  const dart = readFileSync('client/lib/contract.dart', 'utf8');
  for (const e of endpoints) {
    if (e.response === 'binary') continue;
    const listMatch = e.response.match(/^list<(.+)>$/);
    const typeName = listMatch ? listMatch[1] : e.response;
    const fields = types.get(typeName);
    assert.ok(fields, e.id + ' response 类型未定义: ' + typeName);
    assert.match(go, new RegExp('type ' + typeName + ' struct'), 'Go 缺少 ' + typeName);
    assert.match(ts, new RegExp('export (?:interface|type) ' + typeName + '\\b'), 'TS 缺少 ' + typeName);
    assert.match(dart, new RegExp('class ' + typeName + '\\b'), 'Dart 缺少 ' + typeName);
    for (const f of fields) {
      assert.ok(go.includes('json:"' + f.field), 'Go ' + typeName + ' 缺字段 ' + f.field);
      assert.ok(ts.includes(f.field), 'TS ' + typeName + ' 缺字段 ' + f.field);
      assert.ok(dart.includes("'" + f.field + "'"), 'Dart ' + typeName + ' 缺字段 ' + f.field);
    }
  }
});

test('TestBinaryResponseIsDocumented', () => {
  const gw15 = endpoints.find((e) => e.id === 'GW-15');
  assert.equal(gw15.response, 'binary');
  assert.ok(CONTRACT.includes('octet-stream'), 'binary 语义必须在契约里写明');
});
