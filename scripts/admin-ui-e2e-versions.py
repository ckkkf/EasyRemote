#!/usr/bin/env python3
"""ADM-UI-09d E2E：A11 版本管理（发布草稿/灰度/校验/下线/权限守卫）。

真实 stack + 真实 admin API + 真实构建产物。
输出 docs/impl/evidence/ADM-UI-09d/e2e-versions.json + 截图，末行打印 E2E OK。
"""
import base64, hashlib, hmac, http.server, json, os, socketserver, struct, subprocess, sys, threading, time, urllib.error, urllib.request
from admin_e2e import pick

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BIN = "/tmp/er-admui09d-stack"
STACK = "http://127.0.0.1:8302"
SPORT = 5404
DIST = os.path.join(ROOT, "web-admin", "dist")
OUT = os.path.join(ROOT, "docs/impl/evidence/ADM-UI-09d")
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

    st = subprocess.Popen([BIN, "-addr", ":8302"], stdout=open("/tmp/er-admui09d-stack.log", "w"),
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

            page.goto(base + "/versions", wait_until="networkidle")
            body = page.locator("body").inner_text()
            check("nav shows versions page", "版本管理" in body)
            check("versions start empty", "还没有发布过客户端版本" in body)

            # 发布草稿
            page.locator("button", has_text="发布版本").click()
            pick(page, "ver-platform", "windows")
            page.locator("#ver-version").fill("0.2.14")
            pick(page, "ver-status", "draft")
            page.locator("[role=dialog] button", has_text="确认发布").click()
            page.wait_for_selector('table[aria-label="版本列表"] tbody tr', timeout=15000)
            row = page.locator('table[aria-label="版本列表"] tbody tr').first.inner_text()
            check("draft published", "0.2.14" in row and "草稿" in row, row.replace("\n", " | "))
            check("draft rollout is 0", "0%" in row, row.replace("\n", " | "))

            # 前端预校验：全量必须 100%
            page.locator("button", has_text="发布版本").click()
            page.locator("#ver-version").fill("0.2.15")
            pick(page, "ver-status", "full")
            page.locator("#ver-percent").fill("50")
            check("full with 50% blocked locally",
                  page.locator("[role=dialog] button", has_text="确认发布").is_disabled())
            check("rollout error explained",
                  "全量版本覆盖率必须为 100" in page.locator("[role=dialog]").inner_text())
            page.screenshot(path=os.path.join(OUT, "versions-validation.png"))

            # 改成灰度 20% 并发布
            pick(page, "ver-status", "rolling")
            page.locator("#ver-percent").fill("20")
            page.locator("[role=dialog] button", has_text="确认发布").click()
            page.wait_for_function(
                "() => [...document.querySelectorAll('table[aria-label=\"版本列表\"] tbody tr')]"
                ".some(r => r.innerText.includes('0.2.15') && r.innerText.includes('20%'))",
                timeout=15000)
            check("rolling published with 20%", True)
            check("publish writes audit",
                  sum(1 for a in call("/admin/v1/audit", token=token)["items"] if a["action"] == "version.publish") >= 2)
            page.screenshot(path=os.path.join(OUT, "versions-list.png"))

            # 下线（原因必填）
            page.locator('table[aria-label="版本列表"] tbody tr').filter(has_text="0.2.14").first \
                .locator("button", has_text="下线").click()
            page.locator("[role=dialog]").wait_for(state="visible", timeout=10000)
            check("retire requires reason",
                  page.locator("[role=dialog] button", has_text="确认下线").is_disabled())
            page.locator("[role=dialog] #ver-reason").fill("回归测试版本")
            page.locator("[role=dialog] button", has_text="确认下线").click()
            page.locator("[role=dialog]").wait_for(state="hidden", timeout=15000)
            page.wait_for_function(
                "() => [...document.querySelectorAll('table[aria-label=\"版本列表\"] tbody tr')]"
                ".some(r => r.innerText.includes('0.2.14') && r.innerText.includes('已下线'))",
                timeout=15000)
            check("retire marks version retired", True)
            check("retire writes audit",
                  any(a["action"] == "version.retire" for a in call("/admin/v1/audit", token=token)["items"]))
            page.screenshot(path=os.path.join(OUT, "versions-retired.png"))

            page.reload(wait_until="networkidle")
            page.wait_for_selector('table[aria-label="版本列表"] tbody tr', timeout=15000)
            check("deep link survives reload", page.url.endswith("/versions"))

            # 权限守卫
            ctx2 = br.new_context(viewport=VIEWPORT, locale="zh-CN")
            ident2 = json.dumps({"username": "cs1", "role": "cs", "permissions": ["session.read"]})
            ctx2.add_init_script("sessionStorage.setItem('er.admin.token', %s);"
                                 "sessionStorage.setItem('er_admin_identity', %s);"
                                 % (json.dumps(token), json.dumps(ident2)))
            ctx2.route("**/admin/v1/**", forward)
            page2 = ctx2.new_page()
            page2.goto(base + "/versions", wait_until="networkidle")
            text2 = page2.locator("body").inner_text()
            check("no versions nav without permission", "版本管理" not in text2)
            check("forbidden on deep link", "无权限" in text2, text2.replace("\n", " | ")[:100])
            ctx2.close()
            br.close()

        json.dump({"checks": checks, "pageErrors": errors},
                  open(os.path.join(OUT, "e2e-versions.json"), "w"), ensure_ascii=False, indent=2)
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
