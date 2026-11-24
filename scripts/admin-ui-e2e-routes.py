#!/usr/bin/env python3
"""ADM-UI-02a E2E：路由、深链、刷新保持、权限入口与未登录守卫。

真实 stack + 真实 admin API + 真实构建产物；不 mock 任何接口。
输出 docs/impl/evidence/ADM-UI-02a/e2e-routes.json，末行打印 E2E OK。
"""
import base64, functools, hashlib, hmac, http.server, json, os, socketserver, struct, subprocess, sys, threading, time, urllib.error, urllib.request

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BIN = "/tmp/er-admui02a-stack"
STACK = "http://127.0.0.1:8299"
SPORT = 5394
DIST = os.path.join(ROOT, "web-admin", "dist")
OUT = os.path.join(ROOT, "docs/impl/evidence/ADM-UI-02a")
VIEWPORT = {"width": 1360, "height": 860}
TOTP_SECRET = os.environ.get("ER_ADMIN_TOTP_SECRET", "DEVTOTPSECRET")


def call(path, data=None, method=None, token=None, dtoken=None):
    body = json.dumps(data).encode() if data is not None else None
    m = method or ("POST" if body else "GET")
    req = urllib.request.Request(STACK + path, data=body, method=m)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    if dtoken:
        req.add_header("X-Device-Token", dtoken)
    with urllib.request.urlopen(req) as resp:
        return json.loads(resp.read().decode()).get("data", {})


def totp(secret=TOTP_SECRET):
    s = secret + "=" * ((8 - len(secret) % 8) % 8)
    dig = hmac.new(base64.b32decode(s), struct.pack(">Q", int(time.time()) // 30), hashlib.sha256).digest()
    off = dig[-1] & 0x0F
    v = (dig[off] & 0x7F) << 24 | dig[off + 1] << 16 | dig[off + 2] << 8 | dig[off + 3]
    return str(v % 1000000).zfill(6)


def main():
    os.makedirs(OUT, exist_ok=True)
    subprocess.run(["go", "build", "-o", BIN, "./cmd/stack"], cwd=os.path.join(ROOT, "server"), check=True)
    r = subprocess.run(["npm", "run", "build"], cwd=os.path.join(ROOT, "web-admin"),
                       stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    if r.returncode != 0:
        print(r.stdout)
        return 1

    st = subprocess.Popen([BIN, "-addr", ":8299"], stdout=open("/tmp/er-admui02a-stack.log", "w"),
                          stderr=subprocess.STDOUT)
    try:
        for _ in range(80):
            try:
                call("/healthz")
                break
            except Exception:
                time.sleep(0.2)

        # 夹具：一个账号一台设备，保证列表页不是空态（空态另有断言，不混用）。
        call("/v1/auth/sms", {"phone": "13900002001"})
        tok = call("/v1/auth/login", {"phone": "13900002001", "code": "123456"})["token"]
        call("/v1/devices/bind", {"platform": "macOS"}, token=tok)
        admin = call("/admin/v1/login", {"username": "root", "password": "dev-pass", "totp": totp()})

        class H(http.server.SimpleHTTPRequestHandler):
            def __init__(self, *a, **kw):
                super().__init__(*a, directory=DIST, **kw)

            def log_message(self, *a):
                pass

            def send_head(self):
                # SPA fallback：/users 这类深链在静态服务器上没有对应文件，回 index.html。
                target = self.translate_path(self.path)
                if not os.path.exists(target):
                    self.path = "/index.html"
                return super().send_head()

        class TS(socketserver.ThreadingTCPServer):
            allow_reuse_address = True

        srv = TS(("127.0.0.1", SPORT), H)
        threading.Thread(target=srv.serve_forever, daemon=True).start()

        def forward(route):
            req = route.request
            url = req.url.replace("http://127.0.0.1:%d" % SPORT, STACK)
            body = req.post_data.encode() if req.post_data else None
            fr = urllib.request.Request(url, data=body, method=req.method)
            for k, v in req.headers.items():
                if k.lower() in ("host", "content-length", "accept-encoding", "connection", "origin", "referer"):
                    continue
                fr.add_header(k, v)
            try:
                with urllib.request.urlopen(fr) as resp:
                    bt, sc, ct = resp.read(), resp.status, resp.headers.get("Content-Type", "application/json")
            except urllib.error.HTTPError as e:
                bt, sc, ct = e.read(), e.code, e.headers.get("Content-Type", "application/json")
            route.fulfill(status=sc, body=bt, headers={"Content-Type": ct})

        from playwright.sync_api import sync_playwright
        checks = []
        errors = []

        def check(name, cond, detail=""):
            checks.append({"name": name, "ok": bool(cond), "detail": detail})
            print(("PASS " if cond else "FAIL ") + name + ((" -> " + str(detail)) if detail else ""))

        with sync_playwright() as p:
            br = p.chromium.launch(channel="chrome", headless=True)

            def ctx_with_token():
                ctx = br.new_context(viewport=VIEWPORT, locale="zh-CN")
                ident = json.dumps({"username": admin["username"], "role": admin["role"],
                                    "permissions": admin["permissions"]})
                ctx.add_init_script(
                    "sessionStorage.setItem('er.admin.token', %s);"
                    "sessionStorage.setItem('er_admin_identity', %s);"
                    % (json.dumps(admin["token"]), json.dumps(ident)))
                ctx.route("**/admin/v1/**", forward)
                return ctx

            base = "http://127.0.0.1:%d" % SPORT
            ctx = ctx_with_token()
            page = ctx.new_page()
            page.on("pageerror", lambda e: errors.append(str(e)))

            # 1) 深链直达 /users
            page.goto(base + "/users", wait_until="networkidle")
            page.wait_for_selector("table[data-testid=data-table] tbody tr", timeout=15000)
            check("deep link /users renders page", page.locator("h2").first.inner_text() == "用户",
                  page.locator("h2").first.inner_text())
            check("deep link /users keeps URL", page.url.endswith("/users"), page.url)
            rows = page.locator("table[data-testid=data-table] tbody tr").count()
            check("deep link /users has data rows", rows > 0, rows)

            # 2) 刷新后仍在同一页（不再回仪表盘）
            page.reload(wait_until="networkidle")
            page.wait_for_selector("table[data-testid=data-table] tbody tr", timeout=15000)
            check("reload keeps route", page.url.endswith("/users"), page.url)
            check("reload keeps page title", page.locator("h2").first.inner_text() == "用户")

            # 3) /config（root 有 config.read）
            page.goto(base + "/config", wait_until="networkidle")
            page.wait_for_selector("table[data-testid=data-table] input.cfg-input", timeout=15000)
            check("deep link /config renders config table", page.locator("table[data-testid=data-table] input.cfg-input").count() > 0)
            check("deep link /config title", page.locator("h2").first.inner_text() == "配置中心")

            # 4) 未知路径 → 404 页，不是白屏
            page.goto(base + "/nope", wait_until="networkidle")
            page.wait_for_selector("h2", timeout=15000)
            check("unknown path renders notfound", page.locator("h2").first.inner_text() == "页面不存在",
                  page.locator("h2").first.inner_text())

            # 5) 导航点击改变 URL
            page.locator("nav a.nav", has_text="审计日志").first.click()
            page.wait_for_selector("h2", timeout=15000)
            check("nav click updates URL", page.url.endswith("/audit"), page.url)
            check("nav click updates title", page.locator("h2").first.inner_text() == "审计日志")
            active = page.locator("nav a.nav.active").first.inner_text()
            check("nav active state follows route", "审计日志" in active, active)

            # 6) 已登录访问 /login → 回仪表盘
            page.goto(base + "/login", wait_until="networkidle")
            page.wait_for_selector(".metrics .metric", timeout=15000)
            check("/login redirects when authenticated", page.url.endswith("/dashboard"), page.url)

            # 7) 未登录深链 → 登录页，且不渲染任何业务内容
            ctx2 = br.new_context(viewport=VIEWPORT, locale="zh-CN")
            ctx2.route("**/admin/v1/**", forward)
            page2 = ctx2.new_page()
            page2.on("pageerror", lambda e: errors.append("anon: " + str(e)))
            page2.goto(base + "/users", wait_until="networkidle")
            page2.wait_for_selector("#login-username", timeout=15000)
            check("anon deep link lands on login", page2.url.endswith("/login"), page2.url)
            check("anon sees no admin table", page2.locator("table[data-testid=data-table]").count() == 0)
            ctx2.close()
            ctx.close()
            br.close()
        srv.shutdown()

        result = {
            "generatedAt": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
            "viewport": VIEWPORT,
            "baseUrl": base,
            "admin": {"username": admin["username"], "role": admin["role"]},
            "checks": checks,
            "pageErrors": errors,
        }
        with open(os.path.join(OUT, "e2e-routes.json"), "w", encoding="utf-8") as f:
            json.dump(result, f, ensure_ascii=False, indent=2)
            f.write("\n")

        failed = [c["name"] for c in checks if not c["ok"]]
        if failed or errors:
            print("FAILED:", failed, errors)
            return 1
        print("E2E OK (%d checks)" % len(checks))
        return 0
    finally:
        st.terminate()
        try:
            st.wait(timeout=5)
        except subprocess.TimeoutExpired:
            st.kill()


if __name__ == "__main__":
    sys.exit(main())
