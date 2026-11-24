#!/usr/bin/env python3
"""ADM-UI-05 E2E：用户列表 DataTable、详情独立路由/深链、未开放能力显式禁用。

真实 stack + 真实 admin API + 真实构建产物。
输出 docs/impl/evidence/ADM-UI-05/e2e-users.json + 截图，末行打印 E2E OK。
"""
import base64, functools, hashlib, hmac, http.server, json, os, socketserver, struct, subprocess, sys, threading, time, urllib.error, urllib.request

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BIN = "/tmp/er-admui05-stack"
STACK = "http://127.0.0.1:8299"
SPORT = 5398
DIST = os.path.join(ROOT, "web-admin", "dist")
OUT = os.path.join(ROOT, "docs/impl/evidence/ADM-UI-05")
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
    """账号 A 绑 2 台设备并造一次会话，账号 B 绑 1 台；返回账号 A 的会话与设备信息。"""
    ids = []
    fixture = {}
    for i in range(2):
        phone = "139000050%02d" % i
        call("/v1/auth/sms", {"phone": phone})
        login = call("/v1/auth/login", {"phone": phone, "code": "123456"})
        tok = login["token"]
        bound = []
        for p in (["macOS", "Windows"] if i == 0 else ["Android"]):
            bound.append(call("/v1/devices/bind", {"platform": p}, token=tok))
        me = call("/v1/devices", token=tok)
        ids.append(me["devices"][0]["device_code"])
        if i == 0:
            fixture = {"account_id": login["account_id"], "token": tok,
                       "device_token": bound[0]["device_token"],
                       "target_device_id": bound[1]["device_id"],
                       "target_code": bound[1]["raw_code"]}
    # 账号 A 内部直连一次并关闭，供详情页「最近会话」展示。
    call("/v1/devices/password", {"device_id": fixture["target_device_id"],
                                  "password": "userspass1"}, token=fixture["token"])
    sess = call("/v1/connect/request", {"device_code": fixture["target_code"], "captcha": "userspass1"},
                token=fixture["token"], dtoken=fixture["device_token"])
    call("/v1/sessions/close", {"session_id": sess["session_id"], "reason": "user hangup"},
         token=fixture["token"], dtoken=fixture["device_token"])
    return {"ids": ids, "account_id": fixture["account_id"]}


def main():
    os.makedirs(OUT, exist_ok=True)
    subprocess.run(["go", "build", "-o", BIN, "./cmd/stack"], cwd=os.path.join(ROOT, "server"), check=True)
    r = subprocess.run(["npm", "run", "build"], cwd=os.path.join(ROOT, "web-admin"),
                       stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    if r.returncode != 0:
        print(r.stdout)
        return 1

    st = subprocess.Popen([BIN, "-addr", ":8299"], stdout=open("/tmp/er-admui05-stack.log", "w"),
                          stderr=subprocess.STDOUT)
    try:
        for _ in range(80):
            try:
                call("/healthz")
                break
            except Exception:
                time.sleep(0.2)
        fx = seed()
        admin = call("/admin/v1/login", {"username": "root", "password": "dev-pass", "totp": totp()})
        token = admin["token"]
        users = call("/admin/v1/users", token=token)
        first_id = users["items"][0]["id"]

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

            # 列表：列、脱敏、未开放入口禁用
            page.goto(base + "/users", wait_until="networkidle")
            page.wait_for_selector("table[data-testid=data-table] tbody tr", timeout=15000)
            headers = page.locator("table[data-testid=data-table] thead th").all_inner_texts()
            check("users table columns",
                  headers == ["账号 ID", "昵称", "手机号", "设备数", "状态", "注册时间", "最近登录", "操作"], headers)
            # ADM-API-03a：设备数由服务端聚合（夹具账号 A 绑 2 台、账号 B 绑 1 台）。
            counts = page.locator("table[data-testid=data-table] tbody tr td:nth-child(4)").all_inner_texts()
            check("device count column from server", sorted(counts) == ["1", "2"], counts)
            # ADM-API-03b：昵称没有编辑接口时为空（显示 —），最近登录由登录路径写入（刚刚/分钟前）。
            nicknames = page.locator("table[data-testid=data-table] tbody tr td:nth-child(2)").all_inner_texts()
            check("nickname empty until set", all(n == "—" for n in nicknames), nicknames)
            last_logins = page.locator("table[data-testid=data-table] tbody tr td:nth-child(7)").all_inner_texts()
            check("last login recorded", all(x and x != "—" for x in last_logins), last_logins)
            masks = page.locator("table[data-testid=data-table] tbody tr td:nth-child(3)").all_inner_texts()
            check("phone masked", all("*" in m for m in masks), masks)
            # ADM-API-03e：注册时间范围（服务端过滤）+ CSV 导出（脱敏 + 审计）。
            page.fill("input[aria-label=注册开始时间]", "2099-01-01T00:00")
            page.wait_for_function(
                "() => document.body.innerText.includes('没有匹配的用户')", timeout=15000)
            check("future range returns no users", True)
            page.fill("input[aria-label=注册开始时间]", "")
            page.wait_for_selector("table[data-testid=data-table] tbody tr", timeout=15000)
            check("clearing range restores rows", page.locator("table[data-testid=data-table] tbody tr").count() >= 2)

            with page.expect_download() as dl_info:
                page.locator("button", has_text="导出 CSV").click()
            dl = dl_info.value
            content = open(dl.path(), encoding="utf-8-sig").read()
            check("csv header exported", content.splitlines()[0].startswith("账号 ID,昵称,手机号"), content.splitlines()[0])
            check("csv masks phone", "139****5000" in content and "13900005000" not in content,
                  " | ".join(content.splitlines()[1:3]))
            page.wait_for_selector("text=已导出", timeout=15000)
            check("export writes audit",
                  any(a["action"] == "user.export" for a in call("/admin/v1/audit", token=token)["items"]))

            # ADM-API-03d：踢下线（原因必填 + 审计）
            page.locator("table[data-testid=data-table] tbody tr").first.locator("button", has_text="踢下线").click()
            page.locator("[role=dialog]").wait_for(state="visible", timeout=10000)
            check("kick requires reason",
                  page.locator("[role=dialog] button", has_text="确认踢下线").is_disabled())
            page.locator("[role=dialog] #user-action-reason").fill("疑似账号被盗")
            page.locator("[role=dialog] button", has_text="确认踢下线").click()
            page.locator("[role=dialog]").wait_for(state="hidden", timeout=15000)
            check("kick writes audit",
                  any(a["action"] == "user.kick" for a in call("/admin/v1/audit", token=token)["items"]))

            # 封禁 → 解封（解封按钮只在 banned 行出现）
            page.locator("table[data-testid=data-table] tbody tr").first.locator("button", has_text="封禁").click()
            page.locator("[role=dialog] #ban-reason").fill("夹具：封禁测试")
            page.locator("[role=dialog] button", has_text="确认封禁").click()
            page.locator("[role=dialog]").wait_for(state="hidden", timeout=15000)
            page.wait_for_selector("table[data-testid=data-table] tbody tr:has-text('banned')", timeout=15000)
            check("ban applied", True)
            page.locator("table[data-testid=data-table] tbody tr:has-text('banned')").first.locator("button", has_text="解封").click()
            page.locator("[role=dialog] #user-action-reason").fill("误判，解封")
            page.locator("[role=dialog] button", has_text="确认解封").click()
            page.locator("[role=dialog]").wait_for(state="hidden", timeout=15000)
            page.wait_for_function(
                "() => ![...document.querySelectorAll('table[data-testid=data-table] tbody tr')].some(r => r.innerText.includes('banned'))",
                timeout=15000)
            check("unban writes audit",
                  any(a["action"] == "user.unban" for a in call("/admin/v1/audit", token=token)["items"]))
            page.screenshot(path=os.path.join(OUT, "users-list.png"))

            # 行点击 → 独立详情路由
            page.locator("table[data-testid=data-table] tbody tr").first.click()
            page.wait_for_selector("[data-page=user-detail]", timeout=15000)
            check("row opens detail route", page.url.endswith("/users/" + first_id), page.url)
            check("detail shows account section", "账号信息" in page.locator("body").inner_text())
            check("detail shows masked phone", page.locator("[data-page=user-detail]").inner_text().count("*") >= 4)
            page.wait_for_selector('table[aria-label="绑定设备"] tbody tr', timeout=15000)
            check("detail lists devices", page.locator('table[aria-label="绑定设备"] tbody tr').count() >= 1,
                  page.locator('table[aria-label="绑定设备"] tbody tr').count())
            # ADM-API-03c：最近会话（服务端按账号过滤）、操作记录（审计）、备注（可写）。
            page.wait_for_selector('table[aria-label="账号最近会话"] tbody tr', timeout=15000)
            check("account sessions listed",
                  page.locator('table[aria-label="账号最近会话"] tbody tr').count() == 1,
                  page.locator('table[aria-label="账号最近会话"] tbody tr').count())
            check("session reason shown", "user hangup" in page.locator('table[aria-label="账号最近会话"]').inner_text())
            page.locator("textarea[aria-label=账号备注]").fill("疑似诈骗，已电话核实")
            page.locator("button", has_text="保存备注").click()
            page.wait_for_selector("text=已保存（写入审计）", timeout=15000)
            check("note saved", True)
            check("note writes audit",
                  any(a["action"] == "user.note" and a.get("object") == fx["account_id"]
                      for a in call("/admin/v1/audit", token=token)["items"]))
            # 备注在 textarea 的 value 里，innerText 看不到。
            page.wait_for_function(
                "() => document.querySelector('textarea[aria-label=\"账号备注\"]')?.value"
                ".includes('疑似诈骗')", timeout=15000)
            check("note persisted in form", True)
            page.screenshot(path=os.path.join(OUT, "user-detail.png"))

            # 深链刷新后仍是详情页
            page.reload(wait_until="networkidle")
            page.wait_for_selector("[data-page=user-detail]", timeout=15000)
            check("deep link survives reload", page.url.endswith("/users/" + first_id) and
                  "账号信息" in page.locator("body").inner_text())

            # 返回列表
            page.locator("button", has_text="返回列表").click()
            page.wait_for_selector("table[data-testid=data-table] tbody tr", timeout=15000)
            check("back returns to list", page.url.endswith("/users"), page.url)

            # 不存在的账号
            page.goto(base + "/users/acc-does-not-exist", wait_until="networkidle")
            page.wait_for_selector("text=用户不存在", timeout=15000)
            check("unknown user shows not found", "用户不存在" in page.locator("body").inner_text())

            br.close()
        srv.shutdown()

        result = {"generatedAt": time.strftime("%Y-%m-%dT%H:%M:%S%z"), "viewport": VIEWPORT,
                  "checks": checks, "pageErrors": errors, "firstUserId": first_id}
        with open(os.path.join(OUT, "e2e-users.json"), "w", encoding="utf-8") as f:
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
