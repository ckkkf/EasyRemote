// ER-004 扫描器自测：每条规则都要有正样本和负样本。
import test from 'node:test';
import assert from 'node:assert/strict';
import { RULES, parseBaseline, scanFiles, serializeBaseline } from '../check-forbidden.mjs';

function countsFor(files) {
  return scanFiles(files.map(([path, content]) => ({ path, content })));
}

test('TestHttpxBypassDetected', () => {
  const counts = countsFor([
    ['server/internal/gateway/x.go', 'json.NewEncoder(w).Encode(v)\nhttp.Error(w, "x", 500)\n'],
    ['server/internal/gateway/x_test.go', 'json.NewEncoder(w)\n'],
    ['server/internal/httpx/httpx.go', 'json.NewEncoder(w)\n'],
  ]);
  assert.equal(counts.get('httpx-bypass:server/internal/gateway/x.go'), 2);
  assert.equal(counts.size, 1, '测试文件与 httpx 自身不得计入');
});

test('TestBareMapDetected', () => {
  const counts = countsFor([
    ['server/internal/adminhttp/a.go', 'return map[string]any{"a": 1}\n'],
    ['server/internal/adminapi/a.go', 'return map[string]any{"a": 1}\n'],
  ]);
  assert.equal(counts.get('bare-map:server/internal/adminhttp/a.go'), 1);
  assert.equal(counts.get('bare-map:server/internal/adminapi/a.go'), undefined);
});

test('TestHardcodedEndpointDetectedAndTestsExcluded', () => {
  const counts = countsFor([
    ['client/lib/pages/x.dart', "final u = 'http://127.0.0.1:19000';\n"],
    ['client/test/x_test.dart', "final u = 'http://127.0.0.1:19000';\n"],
    ['client/lib/pages/ok.dart', "final u = config.gatewayUrl;\n// 文档示例 http://127.0.0.1:19000 不算\n"],
  ]);
  assert.equal(counts.get('hardcoded-endpoint:client/lib/pages/x.dart'), 1);
  assert.equal(counts.size, 1, '测试与纯注释不应命中');
});

test('TestEmptyCatchDetected', () => {
  const counts = countsFor([
    ['client/lib/a.dart', 'try { f(); } catch (_) {}\n'],
    ['client/lib/b.dart', 'try {\n  f();\n} on Exception catch (e) {\n}\n'],
    ['client/lib/c.dart', 'try { f(); } catch (e) { log(e); }\n'],
  ]);
  assert.equal(counts.get('empty-catch:client/lib/a.dart'), 1);
  assert.equal(counts.get('empty-catch:client/lib/b.dart'), 1);
  assert.equal(counts.size, 2);
});

test('TestFixedSleepDetected', () => {
  const counts = countsFor([
    ['client/lib/a.dart', 'await Future.delayed(const Duration(milliseconds: 500));\n'],
    ['client/lib/b.dart', 'await Future<void>.delayed(d);\n'],
    ['server/internal/x.go', 'time.Sleep(2 * time.Second)\n'],
    ['server/internal/x_test.go', 'time.Sleep(2 * time.Second)\n'],
  ]);
  assert.equal(counts.get('fixed-sleep:client/lib/a.dart'), 1);
  assert.equal(counts.get('fixed-sleep:client/lib/b.dart'), 1);
  assert.equal(counts.get('fixed-sleep:server/internal/x.go'), 1);
  assert.equal(counts.size, 3);
});

test('TestFfiPointerBalance', () => {
  const counts = countsFor([
    ['client/lib/a.dart', 'final a = s.toNativeUtf8();\ncalloc.free(a);\n'],
    ['client/lib/b.dart', 'final a = s.toNativeUtf8();\nfinal b = s.toNativeUtf8();\ncalloc.free(a);\n'],
  ]);
  assert.equal(counts.get('ffi-pointer-balance:client/lib/a.dart'), undefined);
  assert.equal(counts.get('ffi-pointer-balance:client/lib/b.dart'), 1);
});

test('TestReportOnlyAndBaselineFormat', () => {
  const counts = new Map([['empty-catch:client/lib/a.dart', 2]]);
  const text = serializeBaseline(counts);
  const back = parseBaseline(text);
  assert.equal(back.get('empty-catch:client/lib/a.dart'), 2);
  assert.throws(() => parseBaseline('{"version":2,"counts":{}}'));
  assert.ok(RULES.length >= 6, '规则必须包含 ER-004 要求的四类');
});
