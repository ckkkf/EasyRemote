// ER-010 路由检查器自测：method/auth/path 任一漂移都必须被发现。
import test from 'node:test';
import assert from 'node:assert/strict';
import { compareRoutes, parseCodePaths, parseContract, parseSpecs } from '../check-routes.mjs';

const CONTRACT = [
  'endpoints:',
  '  GW-01:',
  '    method: POST',
  '    path: /v1/auth/sms',
  '    auth: none',
  '  GW-03:',
  '    method: GET',
  '    path: /v1/devices',
  '    auth: bearer',
  '  ADM-09:',
  '    method: GET',
  '    path: /admin/v1/config',
  '    auth: admin',
  '  ADM-10:',
  '    method: PUT',
  '    path: /admin/v1/config',
  '    auth: admin',
  '',
].join('\n');

const CODE = [
  'mux.HandleFunc("/v1/auth/sms", h)',
  'mux.HandleFunc("/v1/devices", h)',
  'mux.HandleFunc("/admin/v1/config", h)',
  '',
].join('\n');

function specLines(overrides = {}) {
  const m = { 'GW-01': 'POST', 'GW-03': 'GET', 'ADM-09': 'GET', 'ADM-10': 'PUT', ...overrides.method };
  const a = { 'GW-01': 'none', 'GW-03': 'bearer', 'ADM-09': 'admin', 'ADM-10': 'admin', ...overrides.auth };
  const paths = { 'GW-01': '/v1/auth/sms', 'GW-03': '/v1/devices', 'ADM-09': '/admin/v1/config', 'ADM-10': '/admin/v1/config' };
  const rows = Object.keys(paths).map(
    (id) => '  {ID: "' + id + '", Method: "' + m[id] + '", Path: "' + paths[id] + '", Auth: "' + a[id] + '"},',
  );
  return 'var routeSpecs = []httpx.RouteSpec{\n' + rows.join('\n') + '\n}\n';
}

function compare(opts = {}) {
  return compareRoutes(
    parseContract(opts.contract || CONTRACT),
    parseCodePaths(opts.code || CODE),
    parseSpecs(opts.specs || specLines()),
  );
}

test('TestRouteCheckerAcceptsMatchingRoutes', () => {
  assert.deepEqual(compare(), []);
});

test('TestRouteCheckerRejectsMethodDrift', () => {
  const issues = compare({ specs: specLines({ method: { 'GW-01': 'GET' } }) });
  const drift = issues.filter((i) => i.kind === 'method-drift');
  assert.ok(drift.some((i) => i.id === 'GW-01'), '契约缺 GET /v1/auth/sms 必须报 method-drift');
});

test('TestRouteCheckerRejectsAuthDrift', () => {
  const issues = compare({ specs: specLines({ auth: { 'GW-03': 'device' } }) });
  const drift = issues.filter((i) => i.kind === 'auth-drift');
  assert.equal(drift.length, 1);
  assert.equal(drift[0].id, 'GW-03');
  assert.match(drift[0].detail, /bearer.*device/);
});

test('TestRouteCheckerRejectsPathDrift', () => {
  const ghost = compare({ code: 'mux.HandleFunc("/v1/auth/sms", h)\n' });
  assert.ok(ghost.some((i) => i.kind === 'ghost' && i.path === '/v1/devices'));

  const undeclared = compare({ code: CODE + 'mux.HandleFunc("/v1/extra", h)\n' });
  assert.ok(undeclared.some((i) => i.kind === 'undeclared' && i.path === '/v1/extra'));

  const missingSpec = compare({ specs: specLines().replace(/.*ADM-09.*\n/, '') });
  assert.ok(missingSpec.some((i) => i.kind === 'missing-spec' && i.path.startsWith('/admin/v1/config')));
});
