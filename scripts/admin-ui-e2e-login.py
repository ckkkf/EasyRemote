#!/usr/bin/env python3
"""ADM-UI-03 E2E：登录页走真实 TOTP 登录，错误信封完整可见，且不展示开发口令。

真实 stack + 真实 admin API + 真实构建产物。
输出 docs/impl/evidence/ADM-UI-03/e2e-login.json + 两张截图，末行打印 E2E OK。
"""
import base64, functools, hashlib, hmac, http.server, json, os, socketserver, struct, subprocess, sys, threading, time, urllib.error, urllib.request

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BIN = "/tmp/er-admui03-stack"
STACK = "http://127.0.0.1:8299"
SPORT = 5395
DIST = os.path.join(ROOT, "web-admin", "dist")
OUT = os.path.join(ROOT, "docs/impl/evidence/ADM-UI-03")
VIEWPORT = {"width": 1360, "height": 860}
TOTP_SECRET = os.environ.get("ER_ADMIN_TOTP_SECRET", "DEVTOTPSECRET")


def call(path, data=None):
    body = json.dumps(data).encode() if data is not None else None
    req = urllib.request.Request(STACK + path, data=body, method="POST" if body else "GET")
    req.add_header("Content-Type", "application/json")
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

    st = subprocess.Popen([BIN, "-addr", ":8299"], stdout=open("/tmp/er-admui03-stack.log", "w"),
                          stderr=subprocess.STDOUT)
    try:
        for _ in range(80):
            try:
                call("/healthz")
                break
            except Exception:
                time.sleep(0.2)

        class H(http.server.SimpleHTTPRequestHandler):
            def __init__(self, *a, **kw):
                super().__init__(*a, directory=DIST, **kw)

            def log_message(self, *a):
                pass

            def send_head(self):
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
        checks, errors = [], []

        def check(name, cond, detail=""):
            checks.append({"name": name, "ok": bool(cond), "detail": detail})
            print(("PASS " if cond else "FAIL ") + name + ((" -> " + str(detail)) if detail else ""))

        base = "http://127.0.0.1:%d" % SPORT
        with sync_playwright() as p:
            br = p.chromium.launch(channel="chrome", headless=True)
            ctx = br.new_context(viewport=VIEWPORT, locale="zh-CN")
            ctx.route("**/admin/v1/**", forward)
            page = ctx.new_page()
            page.on("pageerror", lambda e: errors.append(str(e)))

            page.goto(base + "/login", wait_until="networkidle")
            page.wait_for_selector("form button[type=submit]", timeout=15000)

            body_text = page.locator("body").inner_text()
            check("login page has no dev credentials", "dev-pass" not in body_text and "ER_ADMIN_TOTP_OFF" not in body_text)
            check("login page labels are bound", page.locator("label[for=login-username]").count() == 1
                  and page.locator("label[for=login-password]").count() == 1
                  and page.locator("label[for=login-totp]").count() == 1)
            page.screenshot(path=os.path.join(OUT, "login.png"))

            # 1) 错误口令：必须拿到结构化错误信封（code/name/message/trace_id），不是笼统失败。
            page.fill("#login-username", "root")
            page.fill("#login-password", "wrong-pass")
            page.fill("#login-totp", totp())
            page.click("form button[type=submit]")
            page.wait_for_selector("[role=alert]", timeout=15000)
            alert = page.locator("[role=alert]").first.inner_text()
            check("wrong password shows envelope", "AUTH_INVALID_CREDENTIAL" in alert and "100001" in alert, alert.replace("\n", " | "))
            check("wrong password keeps trace_id", "复制 trace_id" in alert)
            check("wrong password stays on login", page.url.endswith("/login"), page.url)
            page.screenshot(path=os.path.join(OUT, "login-error.png"))

            # 2) 正确凭据 + 真实 TOTP：进入控制台。
            page.fill("#login-password", "dev-pass")
            page.fill("#login-totp", totp())
            page.click("form button[type=submit]")
            page.wait_for_selector(".metrics .metric", timeout=20000)
            check("valid credentials reach dashboard", page.url.endswith("/dashboard"), page.url)
            # 指标卡数量随分组扩展（规模/质量/稳定性），这里只要求"至少 4 张 + 三组都在"。
            check("dashboard rendered after login", page.locator(".metrics .metric").count() >= 4,
                  page.locator(".metrics .metric").count())
            check("metric groups rendered after login",
                  all(page.locator("[data-group=%s]" % g).count() == 1 for g in ("scale", "quality", "stability")))
            check("no dev credentials after login", "dev-pass" not in page.locator("body").inner_text())
            page.screenshot(path=os.path.join(OUT, "dashboard-after-login.png"))

            br.close()
        srv.shutdown()

        result = {
            "generatedAt": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
            "viewport": VIEWPORT,
            "baseUrl": base,
            "checks": checks,
            "pageErrors": errors,
        }
        with open(os.path.join(OUT, "e2e-login.json"), "w", encoding="utf-8") as f:
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
