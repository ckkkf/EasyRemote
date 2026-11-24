#!/usr/bin/env python3
"""ADM-UI-10b E2E：服务端分页翻页 + 服务端登出（令牌真的失效）。

真实 stack + 真实 admin API + 真实构建产物。
输出 docs/impl/evidence/ADM-UI-10b/e2e-lifecycle.json + 截图，末行打印 E2E OK。
"""
import base64, functools, hashlib, hmac, http.server, json, os, socketserver, struct, subprocess, sys, threading, time, urllib.error, urllib.request

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BIN = "/tmp/er-admui10b-stack"
STACK = "http://127.0.0.1:8299"
SPORT = 5403
DIST = os.path.join(ROOT, "web-admin", "dist")
OUT = os.path.join(ROOT, "docs/impl/evidence/ADM-UI-10b")
VIEWPORT = {"width": 1360, "height": 860}
TOTP_SECRET = os.environ.get("ER_ADMIN_TOTP_SECRET", "DEVTOTPSECRET")


def call(path, data=None, method=None, token=None):
    body = json.dumps(data).encode() if data is not None else None
    m = method or ("POST" if body else "GET")
    req = urllib.request.Request(STACK + path, data=body, method=m)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    with urllib.request.urlopen(req) as resp:
        return json.loads(resp.read().decode()).get("data", {})


def raw_status(path, token):
    """返回 (http_status, envelope_name)，用于验证登出后令牌真的失效。"""
    req = urllib.request.Request(STACK + path, method="GET")
    req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req) as resp:
            body = json.loads(resp.read().decode())
            return resp.status, body.get("name")
    except urllib.error.HTTPError as e:
        try:
            body = json.loads(e.read().decode())
        except Exception:
            body = {}
        return e.code, body.get("name")


def totp(secret=TOTP_SECRET):
    s = secret + "=" * ((8 - len(secret) % 8) % 8)
    dig = hmac.new(base64.b32decode(s), struct.pack(">Q", int(time.time()) // 30), hashlib.sha256).digest()
    off = dig[-1] & 0x0F
    v = (dig[off] & 0x7F) << 24 | dig[off + 1] << 16 | dig[off + 2] << 8 | dig[off + 3]
    return str(v % 1000000).zfill(6)


def seed_users(n):
    for i in range(n):
        phone = "1390001%04d" % i
        call("/v1/auth/sms", {"phone": phone})
        call("/v1/auth/login", {"phone": phone, "code": "123456"})


def main():
    os.makedirs(OUT, exist_ok=True)
    subprocess.run(["go", "build", "-o", BIN, "./cmd/stack"], cwd=os.path.join(ROOT, "server"), check=True)
    r = subprocess.run(["npm", "run", "build"], cwd=os.path.join(ROOT, "web-admin"),
                       stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    if r.returncode != 0:
        print(r.stdout)
        return 1

    # 夹具需要 12 个账号，而网关的 sms:ip 限流是 10 次/小时；本用例验证的是分页与登出，
    # 不是限流本身，所以显式关闭限流（该开关在生产配置下会直接启动失败，见 config.ValidateDeployment）。
    env = dict(os.environ, ER_RATELIMIT_OFF="1")
    st = subprocess.Popen([BIN, "-addr", ":8299"], stdout=open("/tmp/er-admui10b-stack.log", "w"),
                          stderr=subprocess.STDOUT, env=env)
    try:
        for _ in range(80):
            try:
                call("/healthz")
                break
            except Exception:
                time.sleep(0.2)
        seed_users(12)
        admin = call("/admin/v1/login", {"username": "root", "password": "dev-pass", "totp": totp()})
        token = admin["token"]

        class H(http.server.SimpleHTTPRequestHandler):
            def __init__(self, *a, **kw):
                super().__init__(*a, directory=DIST, **kw)

            def log_message(self, *a):
                pass

            def send_head(self):
                if not os.path.exists(self.translate_path(self.path)):
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
        checks, errors = [], []

        def check(name, cond, detail=""):
            checks.append({"name": name, "ok": bool(cond), "detail": detail})
            print(("PASS " if cond else "FAIL ") + name + ((" -> " + str(detail)) if detail else ""))

        base = "http://127.0.0.1:%d" % SPORT
        with sync_playwright() as p:
            br = p.chromium.launch(channel="chrome", headless=True)
            ctx = br.new_context(viewport=VIEWPORT, locale="zh-CN")
            ident = json.dumps({"username": admin["username"], "role": admin["role"],
                                "permissions": admin["permissions"]})
            ctx.add_init_script("sessionStorage.setItem('er.admin.token', %s);"
                                "sessionStorage.setItem('er_admin_identity', %s);"
                                % (json.dumps(token), json.dumps(ident)))
            ctx.route("**/admin/v1/**", forward)
            page = ctx.new_page()
            page.on("pageerror", lambda e: errors.append(str(e)))

            # ---- 服务端分页 ----
            page.goto(base + "/users", wait_until="networkidle")
            page.wait_for_selector("table[data-testid=data-table] tbody tr", timeout=15000)
            pager = page.locator(".pager").inner_text()
            rows1 = page.locator("table[data-testid=data-table] tbody tr td:first-child").all_inner_texts()
            check("page 1 has page size rows", len(rows1) == 10, len(rows1))
            check("pager reports 12 total / 2 pages", "共 12 条" in pager and "第 1 / 2 页" in pager, pager)
            page.screenshot(path=os.path.join(OUT, "users-page1.png"))

            # 用 .pager-btns 的固定顺序定位，并等真实请求 offset=10 回来，避免"点了但没翻页"的假通过。
            next_btn = page.locator(".pager-btns button").nth(1)
            with page.expect_response(lambda r: "/admin/v1/users" in r.url and "offset=10" in r.url, timeout=15000):
                next_btn.click()
            page.wait_for_function(
                "() => document.querySelector('.pager') && document.querySelector('.pager').innerText.includes('第 2 / 2 页')",
                timeout=15000)
            rows2 = page.locator("table[data-testid=data-table] tbody tr td:first-child").all_inner_texts()
            check("page 2 has remaining rows", len(rows2) == 2, len(rows2))
            check("page 2 rows differ from page 1", set(rows1).isdisjoint(set(rows2)), rows2)
            page.screenshot(path=os.path.join(OUT, "users-page2.png"))

            page.locator(".pager-btns button").nth(0).click()
            # 分页文案先变、行数据后到：等行数恢复 10 再比对，否则读到的是上一帧。
            page.wait_for_function(
                "() => document.querySelectorAll('table[data-testid=data-table] tbody tr').length === 10",
                timeout=15000)
            rows_back = page.locator("table[data-testid=data-table] tbody tr td:first-child").all_inner_texts()
            check("back to page 1 restores rows", rows_back == rows1, len(rows_back))

            # ---- 服务端登出 ----
            before_status, before_name = raw_status("/admin/v1/users", token)
            check("token valid before logout", before_status == 200, "%s %s" % (before_status, before_name))

            page.locator("button", has_text="退出登录").click()
            page.wait_for_selector("#login-username", timeout=15000)
            check("logout lands on login", page.url.endswith("/login"), page.url)
            check("logout clears stored token", page.evaluate("() => sessionStorage.getItem('er.admin.token')") is None)
            check("logout clears identity", page.evaluate("() => sessionStorage.getItem('er_admin_identity')") is None)

            after_status, after_name = raw_status("/admin/v1/users", token)
            check("server session revoked after logout",
                  after_status == 401 and after_name == "ADMIN_SESSION_EXPIRED",
                  "%s %s" % (after_status, after_name))
            page.screenshot(path=os.path.join(OUT, "after-logout.png"))

            # 未登录深链用全新上下文（不复用登出后的页面状态），验证守卫本身。
            ctx2 = br.new_context(viewport=VIEWPORT, locale="zh-CN")
            ctx2.route("**/admin/v1/**", forward)
            page2 = ctx2.new_page()
            page2.on("pageerror", lambda e: errors.append("anon: " + str(e)))
            page2.goto(base + "/users", wait_until="networkidle")
            page2.wait_for_selector("#login-username", timeout=15000)
            check("anonymous deep link to login", page2.url.endswith("/login"), page2.url)
            ctx2.close()

            br.close()
        srv.shutdown()

        result = {"generatedAt": time.strftime("%Y-%m-%dT%H:%M:%S%z"), "viewport": VIEWPORT,
                  "checks": checks, "pageErrors": errors}
        with open(os.path.join(OUT, "e2e-lifecycle.json"), "w", encoding="utf-8") as f:
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
