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
