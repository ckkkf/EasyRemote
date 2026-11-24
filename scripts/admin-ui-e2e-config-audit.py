#!/usr/bin/env python3
"""ADM-UI-08 E2E：配置 diff/保存/审计对账、审计动作精确筛选、未开放导出/回滚。

真实 stack + 真实 admin API + 真实构建产物。
输出 docs/impl/evidence/ADM-UI-08/e2e-config-audit.json + 截图，末行打印 E2E OK。
"""
import base64, functools, hashlib, hmac, http.server, json, os, re, socketserver, struct, subprocess, sys, threading, time, urllib.error, urllib.request
from admin_e2e import pick

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BIN = "/tmp/er-admui08-stack"
STACK = "http://127.0.0.1:8299"
SPORT = 5401
DIST = os.path.join(ROOT, "web-admin", "dist")
OUT = os.path.join(ROOT, "docs/impl/evidence/ADM-UI-08")
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

    st = subprocess.Popen([BIN, "-addr", ":8299"], stdout=open("/tmp/er-admui08-stack.log", "w"),
                          stderr=subprocess.STDOUT)
    try:
        for _ in range(80):
            try:
                call("/healthz")
                break
            except Exception:
                time.sleep(0.2)

        # 造一条 user.ban 审计，便于验证动作精确筛选
        call("/v1/auth/sms", {"phone": "13900008000"})
        call("/v1/auth/login", {"phone": "13900008000", "code": "123456"})
        admin = call("/admin/v1/login", {"username": "root", "password": "dev-pass", "totp": totp()})
        token = admin["token"]
        users = call("/admin/v1/users", token=token)
        call("/admin/v1/users/ban", {"account_id": users["items"][0]["id"], "reason": "夹具：审计筛选"}, token=token)

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

            # ---- 配置中心 ----
            page.goto(base + "/config", wait_until="networkidle")
            page.wait_for_selector("table[data-testid=data-table] input.cfg-input", timeout=15000)
            check("no changes initially", "没有改动" in page.locator("body").inner_text())
            check("history card rendered", "变更历史" in page.locator("body").inner_text())

            first = page.locator("table[data-testid=data-table] tbody tr").first
            key = first.locator("td").first.inner_text()
            before = page.locator("input.cfg-input").first.input_value()
            page.locator("input.cfg-input").first.fill("601")
            check("diff lists before -> after", "→ 601" in page.locator("body").inner_text(),
                  key + ": " + before + " -> 601")
            check("save enabled with valid change", page.locator("button", has_text="保存").is_enabled())
            page.screenshot(path=os.path.join(OUT, "config-diff.png"))

            save_btn = page.locator('button', has_text=re.compile(r'^保存 \d+ 项$')).first
            save_btn.wait_for(state='visible', timeout=15000)
            save_btn.click()
            page.wait_for_selector("text=已保存 1 项", timeout=15000)
            audit_api = call("/admin/v1/audit", token=token)
            hit = [a for a in audit_api["items"] if a["action"].startswith("config")]
            check("config save writes audit", len(hit) >= 1,
                  (hit[0]["action"] + " " + str(hit[0].get("before")) + "->" + str(hit[0].get("after"))) if hit else "none")

            # ADM-API-05b：历史里出现本次变更，可一键回滚到旧值（写 config.rollback 审计）
            page.wait_for_function(
                "() => document.body.innerText.includes('720') && document.body.innerText.includes('601')",
                timeout=15000)
            page.locator("button", has_text="回滚到 720").first.click()
            page.wait_for_function(
                "() => { const input = document.querySelector('table[data-testid=data-table] input.cfg-input');"
                " return input && input.value === '720'; }",
                timeout=15000)
            check("rollback restores previous value",
                  page.locator("table[data-testid=data-table] input.cfg-input").first.input_value() == "720",
                  page.locator("table[data-testid=data-table] input.cfg-input").first.input_value())
            audit_rb = call("/admin/v1/audit", token=token)
            check("rollback writes audit", any(a["action"] == "config.rollback" for a in audit_rb["items"]))
            page.screenshot(path=os.path.join(OUT, "config-history.png"))

            # ADM-API-05c1：分组前缀 + 作用域切换（平台覆盖不影响全局）
            page.wait_for_function(
                "() => document.body.innerText.includes('连接 · session.max_minutes')", timeout=15000)
            check("config group shown", True)
            with page.expect_response(lambda r: "/admin/v1/config" in r.url and r.request.method == "GET"):
                pick(page, "配置作用域", "windows")
            page.wait_for_function(
                "() => document.body.innerText.includes('作用域：windows')", timeout=15000)
            # 上一次运行可能已把平台值写成 300：先读当前值，再选一个不同的值，保证"有改动"。
            cur = page.locator("table[data-testid=data-table] input.cfg-input").first.input_value()
            plat = "301" if cur == "300" else "300"
            page.locator("table[data-testid=data-table] input.cfg-input").first.fill(plat)
            save_btn = page.locator('button', has_text=re.compile(r'^保存 \d+ 项$')).first
            save_btn.wait_for(state='visible', timeout=15000)
            save_btn.click()
            page.wait_for_selector("text=已保存 1 项", timeout=15000)
            scoped_audit = [a for a in call("/admin/v1/audit", token=token)["items"]
                            if a["action"] == "config.update" and a.get("object") == "app_config@windows"]
            check("scoped save audits with platform", len(scoped_audit) >= 1,
                  scoped_audit[0].get("object") if scoped_audit else "none")
            with page.expect_response(lambda r: "/admin/v1/config" in r.url and r.request.method == "GET"):
                pick(page, "配置作用域", "__none__")
            page.wait_for_function(
                "() => { const i = document.querySelector('table[data-testid=data-table] input.cfg-input');"
                " return i && i.value === '720'; }", timeout=15000)
            check("global unchanged after platform save",
                  page.locator("table[data-testid=data-table] input.cfg-input").first.input_value() == "720")
            page.screenshot(path=os.path.join(OUT, "config-scope.png"))
            with page.expect_response(lambda r: "/admin/v1/config" in r.url and r.request.method == "GET"):
                pick(page, "配置作用域", "windows")
            page.wait_for_function(
                "() => { const i = document.querySelector('table[data-testid=data-table] input.cfg-input');"
                " return i && i.value === '%s'; }" % plat, timeout=15000)
            check("platform scope keeps its own value", True)

            # ADM-API-05d：版本作用域优先于平台作用域，且审计对象带版本。
            page.fill("input[aria-label=配置版本作用域]", "0.2.13")
            page.wait_for_function(
                "() => { const i = document.querySelector('table[data-testid=data-table] input.cfg-input');"
                " return i && i.value === '%s'; }" % plat, timeout=15000)
            ver = "121" if plat != "121" else "122"
            page.locator("table[data-testid=data-table] input.cfg-input").first.fill(ver)
            save_btn = page.locator('button', has_text=re.compile(r'^保存 \d+ 项$')).first
            save_btn.wait_for(state='visible', timeout=15000)
            save_btn.click()
            page.wait_for_selector("text=已保存 1 项", timeout=15000)
            ver_audit = [a for a in call("/admin/v1/audit", token=token)["items"]
                         if a.get("object") == "app_config@windows@0.2.13"]
            check("version scope audited with platform and version", len(ver_audit) >= 1,
                  ver_audit[0].get("object") if ver_audit else "none")
            page.fill("input[aria-label=配置版本作用域]", "0.2.12")
            page.wait_for_function(
                "() => { const i = document.querySelector('table[data-testid=data-table] input.cfg-input');"
                " return i && i.value === '%s'; }" % plat, timeout=15000)
            check("other version falls back to platform scope", True)
            page.fill("input[aria-label=配置版本作用域]", "")
            pick(page, "配置作用域", "__none__")
            page.wait_for_timeout(300)
            # 非法值：保存禁用 + 明确提示
            page.locator("input.cfg-input").first.fill("0")
            check("invalid value disables save", page.locator("button", has_text="保存").is_disabled())
            check("invalid value explained", "不合法" in page.locator("body").inner_text())
            page.locator("button", has_text="撤销修改").click()

            # ---- 审计 ----
            page.goto(base + "/audit", wait_until="networkidle")
            page.wait_for_selector("table[data-testid=data-table] tbody tr", timeout=15000)
            headers = page.locator("table[data-testid=data-table] thead th").all_inner_texts()
            check("audit columns", headers == ["时间", "操作人", "动作", "对象", "结果", "IP", "变更"], headers)
            # ADM-API-05a：动作选项来自服务端接口，不再用当前页数据造下拉。
            page.locator('[role=combobox][aria-label="动作筛选"]').click()
            page.wait_for_selector("[role=option]", timeout=15000)
            options = page.locator("[role=option]").all_inner_texts()
            page.keyboard.press("Escape")
            check("action options come from server",
                  "user.ban" in options and "config.update" in options, options)
            # ADM-API-05c2：导出 CSV 真实下载 + 服务端脱敏 + 导出写审计。
            check("audit export enabled", page.locator("button", has_text="导出 CSV").is_enabled())
            with page.expect_download() as dl_info:
                page.locator("button", has_text="导出 CSV").click()
            dl = dl_info.value
            path = dl.path()
            content = open(path, encoding="utf-8-sig").read()
            check("csv header exported", content.splitlines()[0].startswith("时间,操作人,角色,动作"), content.splitlines()[0])
            check("csv masks ip", ".x.x" in content, content.splitlines()[1] if len(content.splitlines()) > 1 else "")
            page.wait_for_selector("text=已导出", timeout=15000)
            check("export count shown", "已导出" in page.locator("body").inner_text())
            check("export writes audit",
                  any(a["action"] == "audit.export" for a in call("/admin/v1/audit", token=token)["items"]))

            pick(page, "动作筛选", "user.ban")
            page.wait_for_function(
                "() => { const rows=[...document.querySelectorAll('table[data-testid=data-table] tbody tr')];"
                " return rows.length>0 && rows.every(r => r.children[2].innerText === 'user.ban'); }",
                timeout=15000)
            actions = page.locator("table[data-testid=data-table] tbody tr td:nth-child(3)").all_inner_texts()
            check("exact action filter", all(a == "user.ban" for a in actions), actions)
            page.screenshot(path=os.path.join(OUT, "audit-filter.png"))

            # 操作人精确过滤
            pick(page, "动作筛选", "__none__")
            page.fill("input[aria-label=操作人筛选]", "root")
            page.wait_for_function(
                "() => { const rows=[...document.querySelectorAll('table[data-testid=data-table] tbody tr')];"
                " return rows.length>0 && rows.every(r => r.children[1].innerText === 'root'); }",
                timeout=15000)
            check("actor filter", all(a == "root" for a in page.locator("table[data-testid=data-table] tbody tr td:nth-child(2)").all_inner_texts()))

            # 时间范围：未来起点必须 0 行（服务端过滤，不是前端切片）
            page.fill("input[aria-label=操作人筛选]", "")
            page.fill("input[aria-label=开始时间]", "2099-01-01T00:00")
            page.wait_for_function(
                "() => document.querySelectorAll('table[data-testid=data-table] tbody tr').length === 0", timeout=15000)
            check("future time range returns no rows",
                  "没有匹配的审计记录" in page.locator("body").inner_text())
            page.fill("input[aria-label=开始时间]", "")
            page.wait_for_selector("table[data-testid=data-table] tbody tr", timeout=15000)
            check("clearing filters restores rows", page.locator("table[data-testid=data-table] tbody tr").count() >= 1)

            br.close()
        srv.shutdown()

        result = {"generatedAt": time.strftime("%Y-%m-%dT%H:%M:%S%z"), "viewport": VIEWPORT,
                  "checks": checks, "pageErrors": errors}
        with open(os.path.join(OUT, "e2e-config-audit.json"), "w", encoding="utf-8") as f:
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
