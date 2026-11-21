// 客户端 Web 预览服务器：静态托管 client/build/web，并把 /v1、/admin、/healthz
// 反向代理到一体化服务端，保证浏览器里是同源请求（无需给服务端加 CORS）。
//
//   node scripts/serve-client-web.mjs 127.0.0.1:8090 8091
import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const [upstream = '127.0.0.1:8090', listenPort = '8091'] = process.argv.slice(2);
const [upHost, upPort] = upstream.split(':');
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', 'client', 'build', 'web');

const TYPES = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.mjs': 'text/javascript; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.svg': 'image/svg+xml',
  '.wasm': 'application/wasm',
  '.otf': 'font/otf',
  '.ttf': 'font/ttf',
  '.woff2': 'font/woff2',
  '.ico': 'image/x-icon',
  '.map': 'application/json; charset=utf-8',
};

function proxy(req, res) {
  const up = http.request(
    { host: upHost, port: Number(upPort), path: req.url, method: req.method, headers: { ...req.headers, host: upstream } },
    (r) => { res.writeHead(r.statusCode ?? 502, r.headers); r.pipe(res); },
  );
  // 后端没起/端口不对时，别回纯文本 "bad gateway"：客户端按信封解析会得到
  // SyntaxError，界面只能显示一串 JSON 解析异常。这里按错误信封回，提示才可读。
  up.on('error', () => {
    const payload = JSON.stringify({
      code: 700001,
      name: 'SRV_INTERNAL',
      message: `后端网关不可达（${upstream}）：请先启动 server，或检查代理上游地址`,
      retryable: true,
      trace_id: 'dev-proxy',
    });
    res.writeHead(502, { 'content-type': 'application/json; charset=utf-8' });
    res.end(payload);
  });
  req.pipe(up);
}

http.createServer((req, res) => {
  const url = decodeURIComponent((req.url ?? '/').split('?')[0]);
  if (url.startsWith('/v1/') || url.startsWith('/admin/') || url === '/healthz') return proxy(req, res);

  // 开发服务器不缓存 Flutter Web 产物：Flutter 的 service worker 会把旧的
  // main.dart.js 一直喂给浏览器，改了代码却仍然看到旧错误（"重新构建也没用"）。
  // 这里让浏览器安装一个「自杀」worker：立即接管、注销自己、清掉 CacheStorage
  // 并刷新所有窗口。之后每次刷新都会拿网络上的最新产物。
  if (url === '/flutter_service_worker.js') {
    res.writeHead(200, {
      'content-type': 'application/javascript; charset=utf-8',
      'cache-control': 'no-store',
    });
    res.end(`self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (event) => {
  event.waitUntil((async () => {
    await self.registration.unregister();
    const keys = await caches.keys();
    await Promise.all(keys.map((k) => caches.delete(k)));
    const windows = await self.clients.matchAll({ type: 'window' });
    windows.forEach((client) => client.navigate(client.url));
  })());
});`);
    return;
  }

  let file = path.join(root, url === '/' ? 'index.html' : url);
  if (!file.startsWith(root) || !fs.existsSync(file) || fs.statSync(file).isDirectory()) {
    file = path.join(root, 'index.html');
  }
  res.writeHead(200, {
    'content-type': TYPES[path.extname(file)] ?? 'application/octet-stream',
    'cache-control': 'no-store',
  });
  fs.createReadStream(file).pipe(res);
}).listen(Number(listenPort), '127.0.0.1', () => {
  console.log(`[client-web] http://127.0.0.1:${listenPort}/  →  ${root}  (api → ${upstream})`);
});
