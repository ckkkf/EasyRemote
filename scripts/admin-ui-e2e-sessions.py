#!/usr/bin/env python3
"""ADM-UI-07 E2E：会话状态枚举修复、中断原因类别、详情独立路由、真实关闭+审计。

真实 stack + 真实 admin API + 真实构建产物。
输出 docs/impl/evidence/ADM-UI-07/e2e-sessions.json + 截图，末行打印 E2E OK。
"""
import base64, functools, hashlib, hmac, http.server, json, os, socketserver, struct, subprocess, sys, threading, time, urllib.error, urllib.request
from admin_e2e import pick

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BIN = "/tmp/er-admui07-stack"
STACK = "http://127.0.0.1:8299"
SPORT = 5400
DIST = os.path.join(ROOT, "web-admin", "dist")
OUT = os.path.join(ROOT, "docs/impl/evidence/ADM-UI-07")
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
    call("/v1/auth/sms", {"phone": "13900007000"})
    tok = call("/v1/auth/login", {"phone": "13900007000", "code": "123456"})["token"]
    devs = [call("/v1/devices/bind", {"platform": p}, token=tok)
            for p in ("macOS", "Windows", "Android", "iOS", "Windows")]
    ctrl = devs[0]

    def request(target):
        return call("/v1/connect/request", {"device_code": target["device_code"], "direct": True},
                    token=tok, dtoken=ctrl["device_token"])

    # 先造一条已结束会话，用来验证 closed 筛选（否则筛选后 0 行，无法区分"筛对了"和"筛坏了"）。
    closed_sess = request(devs[4])
    call("/v1/connect/consent", {"session_id": closed_sess["session_id"], "accepted": True},
         dtoken=devs[4]["device_token"])
    call("/v1/sessions/close", {"session_id": closed_sess["session_id"], "reason": "fixture"},
         dtoken=devs[4]["device_token"])

    requested = request(devs[1])
    authorized = request(devs[2])
    call("/v1/connect/consent", {"session_id": authorized["session_id"], "accepted": True},
         dtoken=devs[2]["device_token"])
    active = request(devs[3])
    call("/v1/connect/consent", {"session_id": active["session_id"], "accepted": True},
         dtoken=devs[3]["device_token"])
    call("/v1/sessions/ready", {"session_id": active["session_id"]}, dtoken=ctrl["device_token"])
    call("/v1/sessions/ready", {"session_id": active["session_id"]}, dtoken=devs[3]["device_token"])
    return {"requested": requested["session_id"], "authorized": authorized["session_id"],
            "active": active["session_id"], "closed": closed_sess["session_id"],
            "controller_token": ctrl["device_token"], "target_token": devs[3]["device_token"]}


def main():
    os.makedirs(OUT, exist_ok=True)
    subprocess.run(["go", "build", "-o", BIN, "./cmd/stack"], cwd=os.path.join(ROOT, "server"), check=True)
    r = subprocess.run(["npm", "run", "build"], cwd=os.path.join(ROOT, "web-admin"),
                       stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    if r.returncode != 0:
        print(r.stdout)
        return 1

    st = subprocess.Popen([BIN, "-addr", ":8299"], stdout=open("/tmp/er-admui07-stack.log", "w"),
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

        # ADM-API-02c：用真实设备凭证上报 3 秒采样（不是测试里伪造管理端数据）。
        now = int(time.time())
        call("/v1/sessions/quality", {
            "session_id": fx["active"],
            "samples": [
                {"ts": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(now - 2)), "latency_ms": 28,
                 "loss_permille": 1, "fps": 60, "tier": "hd", "relay": False,
                 "link": "p2p", "target_fps": 60},
                {"ts": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(now - 1)), "latency_ms": 34,
                 "loss_permille": 4, "fps": 58, "tier": "hd", "relay": True,
                 "link": "relay", "target_fps": 60},
                {"ts": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(now)), "latency_ms": 41,
                 "loss_permille": 6, "fps": 55, "tier": "hd", "relay": True,
                 "link": "relay", "target_fps": 60},
            ],
        }, dtoken=fx["controller_token"])

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

            page.goto(base + "/sessions", wait_until="networkidle")
            page.wait_for_selector("table[data-testid=data-table] tbody tr", timeout=15000)
            headers = page.locator("table[data-testid=data-table] thead th").all_inner_texts()
            check("session columns",
                  headers == ["会话 ID", "主控", "被控", "状态", "时长", "平均延迟", "结束原因", "链路", "开始时间", "操作"],
                  headers)
            # ADM-API-02f7：有采样的会话显示平均延迟与主导链路（seed 里 active 会话 3 条采样）。
            pick(page, "状态筛选", "active")
            page.wait_for_function(
                "() => [...document.querySelectorAll('table[data-testid=data-table] tbody tr')]"
                ".some(r => r.innerText.includes('中继') && /\\d+ ms/.test(r.innerText))", timeout=15000)
            check("session latency and link from samples", True)
            pick(page, "状态筛选", "all")
            page.wait_for_selector("table[data-testid=data-table] tbody tr", timeout=15000)

            # ADM-API-03f1：时间范围 / 设备筛选（服务端过滤）。
            page.fill("input[aria-label=开始时间范围]", "2099-01-01T00:00")
            page.wait_for_function(
                "() => document.body.innerText.includes('暂无会话')", timeout=15000)
            check("future time range returns no sessions", True)
            page.fill("input[aria-label=开始时间范围]", "")
            page.wait_for_selector("table[data-testid=data-table] tbody tr", timeout=15000)
            first_ctrl = page.locator("table[data-testid=data-table] tbody tr").first.locator("td").nth(1).inner_text()
            page.fill("input[aria-label=设备筛选]", first_ctrl)
            page.wait_for_function(
                "() => { const rows=[...document.querySelectorAll('table[data-testid=data-table] tbody tr')];"
                " return rows.length>0 && rows.every(r => r.innerText.includes(%s)); }"
                % json.dumps(first_ctrl), timeout=15000)
            check("device filter narrows rows", True, first_ctrl)
            page.fill("input[aria-label=设备筛选]", "")
            page.wait_for_selector("table[data-testid=data-table] tbody tr", timeout=15000)

            # ADM-API-03f2：链路类型筛选（与"链路"列同口径）。
            pick(page, "链路筛选", "relay")
            page.wait_for_function(
                "() => { const rows=[...document.querySelectorAll('table[data-testid=data-table] tbody tr')];"
                " return rows.length>0 && rows.every(r => r.innerText.includes('中继')); }", timeout=15000)
            check("link filter matches link column", True)
            pick(page, "链路筛选", "all")
            page.wait_for_selector("table[data-testid=data-table] tbody tr", timeout=15000)

            # 异常结束筛选：只有已结束会话参与；seed 里的关闭原因是 admin（正常），因此异常结果为空。
            pick(page, "结束类型筛选", "abnormal")
            # seed 的已结束会话 reason="fixture" → 分类 unknown → 算异常；admin 中断的不应出现。
            page.wait_for_function(
                "() => { const rows=[...document.querySelectorAll('table[data-testid=data-table] tbody tr')];"
                " return rows.length>0 && rows.every(r => r.innerText.includes('已结束')"
                " && !r.innerText.includes('admin:')); }", timeout=15000)
            check("abnormal filter returns only abnormal closed sessions", True)
            pick(page, "结束类型筛选", "all")
            page.wait_for_selector("table[data-testid=data-table] tbody tr", timeout=15000)

            # shadcn Select：选项在 portal 里，先展开再读 role=option（ADM-UI-11c）。
            page.locator('[role=combobox][aria-label="状态筛选"]').click()
            page.wait_for_selector('[role=option]', timeout=15000)
            options = page.locator('[role=option]').all_inner_texts()
            page.keyboard.press("Escape")
            check("state filter has machine states",
                  all(any(s in o for o in options) for s in ["requested", "authorized", "active", "closed"]), options)
            check("state filter has no 'ended'", not any("ended" in o for o in options), options)

            # 已结束会话：筛选能筛到、中断按钮禁用
            pick(page, "状态筛选", "closed")
            # 等筛选真正生效（服务端返回后重新渲染），不能只看"有行"。
            page.wait_for_function(
                "() => { const rows=[...document.querySelectorAll('table[data-testid=data-table] tbody tr')];"
                " return rows.length>0 && rows.every(r => (r.children[3]||{}).innerText?.includes('已结束')); }",
                timeout=15000)
            states = page.locator("table[data-testid=data-table] tbody tr td:nth-child(4)").all_inner_texts()
            check("closed filter returns rows", len(states) >= 1, states)
            check("closed rows labelled", all("已结束" in s for s in states), states)
            check("kill disabled on closed row",
                  page.locator("table[data-testid=data-table] tbody tr").first.get_by_role("button", name="中断", exact=True).is_disabled())
            page.screenshot(path=os.path.join(OUT, "sessions-closed-filter.png"))

            # 回到全部，打开 active 会话的中断对话框
            pick(page, "状态筛选", "all")
            page.wait_for_selector("table[data-testid=data-table] tbody tr", timeout=15000)
            row = page.locator("table[data-testid=data-table] tbody tr", has_text=fx["active"]).first
            row.get_by_role("button", name="中断", exact=True).click()
            dlg = page.locator("[role=dialog]")
            dlg.wait_for(state="visible", timeout=10000)
            check("kill dialog requires category",
                  dlg.locator("button", has_text="确认中断").is_disabled())
            pick(page, "kill-category", "user_report")
            dlg.locator("#kill-detail").fill("用户反馈卡死")
            check("kill dialog enabled after category",
                  dlg.locator("button", has_text="确认中断").is_enabled())
            check("kill dialog shows audited reason", "用户投诉：用户反馈卡死" in dlg.inner_text())
            page.screenshot(path=os.path.join(OUT, "kill-dialog.png"))

            # 取消不写审计
            dlg.locator("button", has_text="取消").click()
            dlg.wait_for(state="hidden", timeout=10000)
            audit = call("/admin/v1/audit", token=token)
            check("cancel writes no kill audit", not any(r["action"] == "session.kill" for r in audit["items"]))

            # 确认中断：真实关闭 + 审计
            page.locator("table[data-testid=data-table] tbody tr", has_text=fx["active"]).first.get_by_role(
                "button", name="中断", exact=True).click()
            pick(page, "kill-category", "security")
            page.locator("[role=dialog] button", has_text="确认中断").click()
            page.locator("[role=dialog]").wait_for(state="hidden", timeout=15000)
            audit2 = call("/admin/v1/audit", token=token)
            check("kill writes audit", any(r["action"] == "session.kill" for r in audit2["items"]))
            detail = call("/admin/v1/sessions", token=token)
            closed = next((s for s in detail["items"] if s["id"] == fx["active"]), None)
            check("kill closes session", closed is not None and closed["state"] == "closed", closed)

            # 详情路由
            page.locator("table[data-testid=data-table] tbody tr", has_text=fx["requested"]).first.click()
            page.wait_for_selector("[data-page=session-detail]", timeout=15000)
            check("row opens session detail", page.url.endswith("/sessions/" + fx["requested"]), page.url)
            body = page.locator("body").inner_text()
            check("detail shows session fields", "会话信息" in body and fx["requested"] in body)
            # requested 会话没有采样：必须明确"没有采样"，而不是 0 值曲线。
            check("unsampled session shows no data", "该会话没有采样数据" in body)
            page.screenshot(path=os.path.join(OUT, "session-detail.png"))

            # 有采样的会话：显示真实聚合与采样表
            page.goto(base + "/sessions/" + fx["active"], wait_until="networkidle")
            page.wait_for_selector("[data-page=session-detail]", timeout=15000)
            page.wait_for_selector('table[aria-label="质量采样"] tbody tr', timeout=15000)
            rows = page.locator('table[aria-label="质量采样"] tbody tr').count()
            check("quality samples rendered", rows == 3, rows)
            detail_text = page.locator("body").inner_text()
            check("quality summary computed", "平均延迟" in detail_text and "中继占比" in detail_text)
            check("quality link column shows relay", "中继" in detail_text and "P2P" in detail_text)
            # ADM-API-03h：制造一次真实会话级错误（对已结束会话调用 ready），详情页应展示错误码。
            try:
                call("/v1/sessions/ready", {"session_id": fx["closed"]}, dtoken=fx["controller_token"])
            except urllib.error.HTTPError:
                pass
            page.goto(base + "/sessions/" + fx["closed"], wait_until="networkidle")
            page.wait_for_selector("[data-page=session-detail]", timeout=15000)
            page.wait_for_selector('table[aria-label="错误码时间线"] tbody tr', timeout=15000)
            err_rows = page.locator('table[aria-label="错误码时间线"] tbody tr').count()
            check("session error timeline recorded", err_rows >= 1, err_rows)
            check("error timeline shows code and source",
                  "sessions.ready" in page.locator('table[aria-label="错误码时间线"]').inner_text())
            page.screenshot(path=os.path.join(OUT, "session-errors.png"))

            # 回到有采样的 active 会话继续检查 03g 的三块。
            page.goto(base + "/sessions/" + fx["active"], wait_until="networkidle")
            page.wait_for_selector("[data-page=session-detail]", timeout=15000)

            # ADM-API-03g：双方设备 / 链路切换 / 状态时间线。
            page.wait_for_selector('table[aria-label="会话双方设备"] tbody tr', timeout=15000)
            check("session parties listed",
                  page.locator('table[aria-label="会话双方设备"] tbody tr').count() == 2,
                  page.locator('table[aria-label="会话双方设备"] tbody tr').count())
            page.wait_for_selector('table[aria-label="链路切换"] tbody tr', timeout=15000)
            check("link switch recorded",
                  page.locator('table[aria-label="链路切换"] tbody tr').count() == 1,
                  page.locator('table[aria-label="链路切换"]').inner_text().replace("\n", " | ")[:80])
            page.wait_for_selector('table[aria-label="状态时间线"] tbody tr', timeout=15000)
            check("timeline rendered",
                  page.locator('table[aria-label="状态时间线"] tbody tr').count() >= 1)
            page.screenshot(path=os.path.join(OUT, "session-quality.png"))

            page.reload(wait_until="networkidle")
            page.wait_for_selector("[data-page=session-detail]", timeout=15000)
            check("deep link survives reload", "会话信息" in page.locator("body").inner_text())

            page.locator("button", has_text="返回列表").first.click()
            page.wait_for_selector("table[data-testid=data-table] tbody tr", timeout=15000)
            check("back returns to list", page.url.endswith("/sessions"), page.url)

            # ADM-UI-07b2：双人确认中断（发起人不能自己确认，第二人确认后会话关闭）。
            page.goto(base + "/sessions", wait_until="networkidle")
            page.wait_for_selector("table[data-testid=data-table] tbody tr", timeout=15000)
            pick(page, "状态筛选", "authorized")
            page.wait_for_timeout(600)
            if page.locator("table[data-testid=data-table] tbody tr").count() == 0:
                pick(page, "状态筛选", "requested")
                page.wait_for_timeout(600)
            row = page.locator("table[data-testid=data-table] tbody tr").first
            dual_session = row.locator("td").first.inner_text().replace(" ", "")
            row.locator("button", has_text="双人中断").click()
            page.locator("[role=dialog]").wait_for(state="visible", timeout=10000)
            check("dual request requires reason",
                  page.locator("[role=dialog] button", has_text="提交请求").is_disabled())
            page.locator("[role=dialog] #dual-reason").fill("疑似诈骗会话")
            page.locator("[role=dialog] button", has_text="提交请求").click()
            page.locator("[role=dialog]").wait_for(state="hidden", timeout=15000)
            page.wait_for_selector('table[aria-label="待确认中断"] tbody tr', timeout=15000)
            check("pending request listed", True, dual_session)
            check("dual request writes audit",
                  any(a["action"] == "session.kill_request" for a in call("/admin/v1/audit", token=token)["items"]))

            # 发起人自己确认必须被拒。
            page.locator('table[aria-label="待确认中断"] tbody tr').first.locator(
                "button", has_text="确认中断").click()
            page.locator("[role=dialog]").wait_for(state="visible", timeout=10000)
            page.locator("[role=dialog] button", has_text="确认中断").last.click()
            page.locator("[role=alert]").first.wait_for(state="visible", timeout=15000)
            err = page.locator("[role=alert]").first.inner_text()
            check("requester cannot confirm own request",
                  "ADMIN_FORBIDDEN" in err or "无权限" in err, err.replace("\n", " | ")[:120])
            page.locator("[role=dialog] button", has_text="取消").click()

            # 第二位管理员（ops2-dev）确认后会话关闭。
            admin2 = call("/admin/v1/login", {"username": "ops2-dev", "password": "dev-pass", "totp": totp()})
            ctx2 = br.new_context(viewport=VIEWPORT, locale="zh-CN")
            ident2 = json.dumps({"username": admin2["username"], "role": admin2["role"],
                                 "permissions": admin2["permissions"]})
            ctx2.add_init_script("sessionStorage.setItem('er.admin.token', %s);"
                                 "sessionStorage.setItem('er_admin_identity', %s);"
                                 % (json.dumps(admin2["token"]), json.dumps(ident2)))
            ctx2.route("**/admin/v1/**", forward)
            page2 = ctx2.new_page()
            page2.goto(base + "/sessions", wait_until="networkidle")
            page2.wait_for_selector('table[aria-label="待确认中断"] tbody tr', timeout=15000)
            page2.locator('table[aria-label="待确认中断"] tbody tr').first.locator(
                "button", has_text="确认中断").click()
            page2.locator("[role=dialog] button", has_text="确认中断").last.click()
            page2.locator("[role=dialog]").wait_for(state="hidden", timeout=15000)
            page2.wait_for_function(
                "() => !document.querySelector('table[aria-label=\"待确认中断\"]')", timeout=15000)
            check("second admin confirmed and closed session", True)
            closed = call("/admin/v1/sessions?state=closed", token=admin2["token"])
            check("session closed after confirmation",
                  any(s["id"] == dual_session for s in closed["items"]), dual_session)
            check("confirm writes audit",
                  any(a["action"] == "session.kill_confirm" for a in call("/admin/v1/audit", token=token)["items"]))
            page2.screenshot(path=os.path.join(OUT, "sessions-dual-confirm.png"))
            ctx2.close()
            page.goto(base + "/sessions/ses-does-not-exist", wait_until="networkidle")
            page.wait_for_selector("text=会话不存在", timeout=15000)
            check("unknown session shows not found", "会话不存在" in page.locator("body").inner_text())

            br.close()
        srv.shutdown()

        result = {"generatedAt": time.strftime("%Y-%m-%dT%H:%M:%S%z"), "viewport": VIEWPORT,
                  "checks": checks, "pageErrors": errors, "fixture": fx}
        with open(os.path.join(OUT, "e2e-sessions.json"), "w", encoding="utf-8") as f:
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
