#!/usr/bin/env python3
"""ADM-UI-02c E2E：Radix Dialog 语义（焦点、Esc、aria）+ 封禁/中断真实副作用。

真实 stack + 真实 admin API + 真实构建产物。
输出 docs/impl/evidence/ADM-UI-02c/e2e-dialogs.json + 截图，末行打印 E2E OK。
"""
import base64, functools, hashlib, hmac, http.server, json, os, socketserver, struct, subprocess, sys, threading, time, urllib.error, urllib.request
from admin_e2e import pick

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BIN = "/tmp/er-admui02c-stack"
STACK = "http://127.0.0.1:8299"
SPORT = 5396
DIST = os.path.join(ROOT, "web-admin", "dist")
OUT = os.path.join(ROOT, "docs/impl/evidence/ADM-UI-02c")
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


def seed():
    a = []
    for i in range(2):
        phone = "139000030%02d" % i
        call("/v1/auth/sms", {"phone": phone})
        tok = call("/v1/auth/login", {"phone": phone, "code": "123456"})["token"]
        devs = [call("/v1/devices/bind", {"platform": p}, token=tok)
                for p in (["macOS", "Windows"] if i == 0 else ["Android"])]
        a.append({"token": tok, "devices": devs})
    ctrl, target = a[0]["devices"][0], a[0]["devices"][1]
    s = call("/v1/connect/request", {"device_code": target["device_code"], "direct": True},
             token=a[0]["token"], dtoken=ctrl["device_token"])
    call("/v1/connect/consent", {"session_id": s["session_id"], "accepted": True},
         dtoken=target["device_token"])
    return {"session_id": s["session_id"], "controller_device": ctrl["device_code"]}


def main():
    os.makedirs(OUT, exist_ok=True)
    subprocess.run(["go", "build", "-o", BIN, "./cmd/stack"], cwd=os.path.join(ROOT, "server"), check=True)
    r = subprocess.run(["npm", "run", "build"], cwd=os.path.join(ROOT, "web-admin"),
                       stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    if r.returncode != 0:
        print(r.stdout)
        return 1

    st = subprocess.Popen([BIN, "-addr", ":8299"], stdout=open("/tmp/er-admui02c-stack.log", "w"),
                          stderr=subprocess.STDOUT)
    try:
        for _ in range(80):
            try:
                call("/healthz")
                break
            except Exception:
                time.sleep(0.2)
        fixture = seed()
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

            # ---- 封禁对话框 ----
            page.goto(base + "/users", wait_until="networkidle")
            page.wait_for_selector("table[data-testid=data-table] tbody tr", timeout=15000)
            page.locator("table[data-testid=data-table] tbody tr").first.locator("button", has_text="封禁").click()
            dlg = page.locator("[role=dialog]")
            dlg.wait_for(state="visible", timeout=10000)
            # Radix 不写 aria-modal，而是用 aria-labelledby + 焦点陷阱 + 把外部内容 aria-hidden 表达模态。
            check("ban dialog role=dialog", dlg.get_attribute("role") == "dialog")
            check("ban dialog labelled by title", bool(dlg.get_attribute("aria-labelledby")))
            check("outside content is aria-hidden", page.evaluate(
                "() => document.getElementById('root')?.getAttribute('aria-hidden') === 'true'"))
            check("ban dialog title", "封禁" in dlg.locator("h2").first.inner_text(), dlg.locator("h2").first.inner_text())
            confirm = dlg.locator("button", has_text="确认封禁")
            check("confirm disabled without reason", confirm.is_disabled())
            dlg.locator("#ban-reason").fill("abc")
            check("confirm disabled with 3 chars", confirm.is_disabled())
            dlg.locator("#ban-reason").fill("违规测试原因")
            check("confirm enabled with 4+ chars", confirm.is_enabled())
            check("focus stays inside dialog", page.evaluate(
                "() => { const d=document.querySelector('[role=dialog]'); return !!d && d.contains(document.activeElement); }"))
            page.screenshot(path=os.path.join(OUT, "ban-dialog.png"))

            # Esc 关闭且不发请求
            page.keyboard.press("Escape")
            dlg.wait_for(state="hidden", timeout=10000)
            users_after_esc = call("/admin/v1/users", token=token)
            check("escape closes without side effect",
                  all(u["status"] == "active" for u in users_after_esc["items"]))

            # 真实封禁：对话框关闭 + 审计写入
            page.locator("table[data-testid=data-table] tbody tr").first.locator("button", has_text="封禁").click()
            page.locator("[role=dialog] #ban-reason").fill("违规测试原因")
            page.locator("[role=dialog] button", has_text="确认封禁").click()
            page.locator("[role=dialog]").wait_for(state="hidden", timeout=15000)
            audit = call("/admin/v1/audit", token=token)
            check("ban writes audit", any(r["action"] == "user.ban" for r in audit["items"]))
            users_after_ban = call("/admin/v1/users", token=token)
            check("ban takes effect", any(u["status"] == "banned" for u in users_after_ban["items"]))

            # ---- 中断对话框 ----
            page.goto(base + "/sessions", wait_until="networkidle")
            page.wait_for_selector("table[data-testid=data-table] tbody tr", timeout=15000)
            page.locator("table[data-testid=data-table] tbody tr").first.get_by_role("button", name="中断", exact=True).click()
            page.locator("[role=dialog]").wait_for(state="visible", timeout=10000)
            page.locator("[role=dialog] button", has_text="取消").click()
            page.locator("[role=dialog]").wait_for(state="hidden", timeout=10000)
            audit2 = call("/admin/v1/audit", token=token)
            check("cancel writes no kill audit", not any(r["action"] == "session.kill" for r in audit2["items"]))
            page.locator("table[data-testid=data-table] tbody tr").first.get_by_role("button", name="中断", exact=True).click()
            # 中断必须选原因类别（ADM-UI-07），否则确认按钮保持禁用。
            pick(page, "kill-category", "user_report")
            page.locator("[role=dialog] #kill-detail").fill("用户投诉")
            page.locator("[role=dialog] button", has_text="确认中断").click()
            page.locator("[role=dialog]").wait_for(state="hidden", timeout=15000)
            audit3 = call("/admin/v1/audit", token=token)
            check("kill writes audit", any(r["action"] == "session.kill" for r in audit3["items"]))

            # ---- 行点击导航（ADM-UI-06/05 已把详情从对话框改成独立路由） ----
            page.goto(base + "/devices", wait_until="networkidle")
            page.wait_for_selector("table[data-testid=data-table] tbody tr.cursor-pointer", timeout=15000)
            page.locator("table[data-testid=data-table] tbody tr.cursor-pointer").first.locator("td").first.click()
            page.wait_for_selector("[data-page=device-detail]", timeout=15000)
            check("device row navigates to detail page", "/devices/" in page.url, page.url)
            page.screenshot(path=os.path.join(OUT, "device-detail-page.png"))

            page.goto(base + "/users", wait_until="networkidle")
            page.wait_for_selector("table[data-testid=data-table] tbody tr.cursor-pointer", timeout=15000)
            page.locator("table[data-testid=data-table] tbody tr.cursor-pointer").first.locator("td").first.click()
            page.wait_for_selector("[data-page=user-detail]", timeout=15000)
            check("user row navigates to detail page", "/users/" in page.url, page.url)
            page.wait_for_selector('table[aria-label="绑定设备"]', timeout=15000)
            check("user detail has tables", page.locator("table[data-testid=data-table]").count() >= 1)

            br.close()
        srv.shutdown()

        result = {"generatedAt": time.strftime("%Y-%m-%dT%H:%M:%S%z"), "viewport": VIEWPORT,
                  "checks": checks, "pageErrors": errors, "fixture": fixture}
        with open(os.path.join(OUT, "e2e-dialogs.json"), "w", encoding="utf-8") as f:
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
