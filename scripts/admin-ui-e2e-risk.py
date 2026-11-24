#!/usr/bin/env python3
"""ADM-UI-09c E2E：A10 风控中心（黑名单增删 / 告警 / 校验 / 权限守卫）。

真实 stack + 真实 admin API + 真实网关拦截 + 真实构建产物。
输出 docs/impl/evidence/ADM-UI-09c/e2e-risk.json + 截图，末行打印 E2E OK。
"""
import base64, hashlib, hmac, http.server, json, os, socketserver, struct, subprocess, sys, threading, time, urllib.error, urllib.request
from admin_e2e import pick

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BIN = "/tmp/er-admui09c-stack"
STACK = "http://127.0.0.1:8301"
SPORT = 5403
DIST = os.path.join(ROOT, "web-admin", "dist")
OUT = os.path.join(ROOT, "docs/impl/evidence/ADM-UI-09c")
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


def login_device(phone):
    call("/v1/auth/sms", {"phone": phone})
    tok = call("/v1/auth/login", {"phone": phone, "code": "123456"})
    dev = call("/v1/devices/bind", {"platform": "macOS"}, token=tok["token"])
    return {"token": tok["token"], "account_id": tok["account_id"], "device": dev}


def main():
    os.makedirs(OUT, exist_ok=True)
    subprocess.run(["go", "build", "-o", BIN, "./cmd/stack"], cwd=os.path.join(ROOT, "server"), check=True)
    r = subprocess.run(["npm", "run", "build"], cwd=os.path.join(ROOT, "web-admin"),
                       stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    if r.returncode != 0:
        print(r.stdout)
        return 1

    st = subprocess.Popen([BIN, "-addr", ":8301"], stdout=open("/tmp/er-admui09c-stack.log", "w"),
                          stderr=subprocess.STDOUT)
    try:
        for _ in range(80):
            try:
                call("/healthz")
                break
            except Exception:
                time.sleep(0.2)

        admin = call("/admin/v1/login", {"username": "root", "password": "dev-pass", "totp": totp()})
        token = admin["token"]

        # 造一次被拦截的连接：把主控账号加黑名单，再让它的连接请求被拒绝 → 产生告警。
        ctrl = login_device("13900009101")
        target = login_device("13900009102")
        call("/v1/devices/password", {"device_id": target["device"]["device_id"], "password": "riskpass1"},
             token=target["token"])
        call("/admin/v1/risk/blacklist", {"kind": "account", "value": ctrl["account_id"],
                                          "note": "夹具：拦截产生告警"}, token=token)
        try:
            call("/v1/connect/request", {"device_code": target["device"]["raw_code"], "captcha": "riskpass1"},
                 token=ctrl["token"], dtoken=ctrl["device"]["device_token"])
            blocked = False
        except urllib.error.HTTPError as e:
            blocked = e.code >= 400
        # 清掉夹具黑名单，让页面从空列表开始
        entries = call("/admin/v1/risk/blacklist", token=token)["items"]
        for e in entries:
            call("/admin/v1/risk/blacklist/remove", {"id": e["id"], "reason": "夹具清理"}, token=token)

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

            page.goto(base + "/risk", wait_until="networkidle")
            body = page.locator("body").inner_text()
            check("nav shows risk page", "风控中心" in body)
            check("blocked connect produced alert", blocked, blocked)
            check("blacklist starts empty", "黑名单为空" in body)

            # 非法 IP 必须被服务端拒绝，并在页面上给出错误态
            page.locator("button", has_text="新增黑名单").click()
            pick(page, "风控类型", "ip")
            page.locator("#risk-value").fill("999.1.1.1")
            page.locator("[role=dialog] button", has_text="确认新增").click()
            page.locator("[role=alert]").first.wait_for(state="visible", timeout=15000)
            err = page.locator("[role=alert]").first.inner_text()
            check("invalid ip rejected", "ADMIN_CONFIG_INVALID" in err or "参数" in err,
                  err.replace("\n", " | ")[:120])
            page.locator("[role=dialog] button", has_text="取消").click()

            # 正常新增账号黑名单
            page.locator("button", has_text="新增黑名单").click()
            pick(page, "风控类型", "account")
            page.locator("#risk-value").fill(ctrl["account_id"])
            page.locator("#risk-note").fill("同 IP 高频连接")
            page.locator("#risk-ttl").fill("24")
            page.locator("[role=dialog] button", has_text="确认新增").click()
            page.wait_for_selector('table[aria-label="黑名单列表"] tbody tr', timeout=15000)
            row = page.locator('table[aria-label="黑名单列表"] tbody tr').first.inner_text()
            check("blacklist row added", ctrl["account_id"] in row and "24" not in row.split("\t")[0], row.replace("\n", " | "))
            check("blacklist shows expiry", "永久" not in row, row.replace("\n", " | "))
            check("add writes audit",
                  any(a["action"] == "risk.blacklist_add" for a in call("/admin/v1/audit", token=token)["items"]))
            page.screenshot(path=os.path.join(OUT, "risk-blacklist.png"))

            # 告警列表来自真实拦截
            alerts = page.locator('table[aria-label="风控告警"] tbody tr')
            check("alert listed from real block", alerts.count() >= 1, alerts.count())
            check("alert subject is account", "account:" in page.locator('table[aria-label="风控告警"]').inner_text())

            # 移除黑名单（原因必填）
            page.locator('table[aria-label="黑名单列表"] tbody tr').first.locator("button", has_text="移除").click()
            page.locator("[role=dialog]").wait_for(state="visible", timeout=10000)
            check("remove requires reason",
                  page.locator("[role=dialog] button", has_text="确认移除").is_disabled())
            page.locator("[role=dialog] #risk-reason").fill("误报")
            page.locator("[role=dialog] button", has_text="确认移除").click()
            page.locator("[role=dialog]").wait_for(state="hidden", timeout=15000)
            page.wait_for_function(
                "() => document.body.innerText.includes('黑名单为空')", timeout=15000)
            check("blacklist removed", True)
            check("remove writes audit",
                  any(a["action"] == "risk.blacklist_remove" for a in call("/admin/v1/audit", token=token)["items"]))
            page.screenshot(path=os.path.join(OUT, "risk-after-remove.png"))

            # 权限守卫：无 risk.read 看不到入口，深链 403
            ctx2 = br.new_context(viewport=VIEWPORT, locale="zh-CN")
            ident2 = json.dumps({"username": "cs1", "role": "cs", "permissions": ["session.read"]})
            ctx2.add_init_script("sessionStorage.setItem('er.admin.token', %s);"
                                 "sessionStorage.setItem('er_admin_identity', %s);"
                                 % (json.dumps(token), json.dumps(ident2)))
            ctx2.route("**/admin/v1/**", forward)
            page2 = ctx2.new_page()
            page2.goto(base + "/risk", wait_until="networkidle")
            text2 = page2.locator("body").inner_text()
            check("no risk nav without permission", "风控中心" not in text2)
            check("forbidden on deep link", "无权限" in text2, text2.replace("\n", " | ")[:100])
            ctx2.close()
            br.close()

        json.dump({"checks": checks, "pageErrors": errors},
                  open(os.path.join(OUT, "e2e-risk.json"), "w"), ensure_ascii=False, indent=2)
        bad = [c for c in checks if not c["ok"]]
        if bad:
            print("E2E FAILED:", len(bad))
            return 1
        if errors:
            print("PAGE ERRORS:", errors)
            return 1
        print("E2E OK (%d checks)" % len(checks))
        return 0
    finally:
        st.terminate()
        try:
            st.wait(timeout=5)
        except Exception:
            st.kill()


if __name__ == "__main__":
    sys.exit(main())
