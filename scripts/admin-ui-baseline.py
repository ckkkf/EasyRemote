#!/usr/bin/env python3
"""ADM-UI-00 基线截图脚本：真实 stack + 真实 admin API，1360x860 六屏截图。

前置：go / node / python3-playwright / Chrome。
用法：
  python3 scripts/admin-ui-baseline.py

行为：
  1. go build ./cmd/stack → /tmp/er-admui00-stack，起在 127.0.0.1:8299（内存存储）。
  2. 用真实网关 API 造夹具：6 个账号、8 台设备、requested/active/ended 三种会话。
  3. npm run build 出 web-admin/dist，用 http.server 在 5392 提供，/admin/v1/* 转发到 stack。
  4. 真实 admin 登录（账号密码 + TOTP），token 写入 sessionStorage（与 src/api.ts 一致）。
  5. 采集 A1 登录 / A2 仪表盘 / A3 用户 / A6 设备详情 / A7 会话 / A12 配置中心六屏。
  6. 输出 screens/manifest.json：viewport、字体、主题、夹具、DOM 判据、pageerror。

夹具只用开发账号与合成手机号，不含任何生产数据；本脚本不进入 release 产物。
"""
import base64, functools, hashlib, hmac, http.server, json, os, re, socketserver, struct, subprocess, sys, threading, time, urllib.error, urllib.request

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BIN = "/tmp/er-admui00-stack"
STACK = "http://127.0.0.1:8299"
SPORT = 5392
DIST = os.path.join(ROOT, "web-admin", "dist")


def parse_args():
    """--out DIR 改输出目录；--compare-to manifest.json 与既有基线逐屏比对 DOM 判据。

    默认行为（无参数）仍是 ADM-UI-00 的基线采集，保证文档里的复现命令不变。
    """
    out = os.path.join(ROOT, "docs/impl/evidence/ADM-UI-00/screens")
    compare = ""
    argv = sys.argv[1:]
    i = 0
    while i < len(argv):
        if argv[i] == "--out" and i + 1 < len(argv):
            out = os.path.abspath(argv[i + 1])
            i += 2
        elif argv[i] == "--compare-to" and i + 1 < len(argv):
            compare = os.path.abspath(argv[i + 1])
            i += 2
        else:
            print("unknown arg:", argv[i])
            sys.exit(2)
    return out, compare


OUT, COMPARE_TO = parse_args()
VIEWPORT = {"width": 1360, "height": 860}
ADMIN_USER = os.environ.get("ER_ADMIN_USER", "root")
ADMIN_PASS = os.environ.get("ER_ADMIN_PASS", "dev-pass")
TOTP_SECRET = os.environ.get("ER_ADMIN_TOTP_SECRET", "DEVTOTPSECRET")


def build_stack():
    r = subprocess.run(["go", "build", "-o", BIN, "./cmd/stack"], cwd=os.path.join(ROOT, "server"))
    if r.returncode != 0:
        print("BUILD FAIL")
        sys.exit(1)


def build_web():
    r = subprocess.run(["npm", "run", "build"], cwd=os.path.join(ROOT, "web-admin"),
                       stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    if r.returncode != 0:
        print(r.stdout)
        print("WEB BUILD FAIL")
        sys.exit(1)


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
        env = json.loads(resp.read().decode())
    return env.get("data", env)


def totp(secret=TOTP_SECRET):
    s = secret + "=" * ((8 - len(secret) % 8) % 8)
    dig = hmac.new(base64.b32decode(s), struct.pack(">Q", int(time.time()) // 30), hashlib.sha256).digest()
    off = dig[-1] & 0x0F
    v = (dig[off] & 0x7F) << 24 | dig[off + 1] << 16 | dig[off + 2] << 8 | dig[off + 3]
    return str(v % 1000000).zfill(6)


def seed():
    """用真实网关 API 造夹具：6 账号 / 10 设备 / 4 种会话状态。

    会话按（主控, 被控）唯一：同一个设备对重复 request 会复用旧会话，
    所以四种状态各用一台不同的被控设备，避免 idempotency 把夹具合并掉。
    """
    accounts = []
    for i in range(6):
        phone = "139000000%02d" % i
        call("/v1/auth/sms", {"phone": phone})
        tok = call("/v1/auth/login", {"phone": phone, "code": "123456"})["token"]
        devs = []
        for j in range(5 if i == 0 else 1):
            d = call("/v1/devices/bind", {"platform": ["macOS", "Windows", "Android", "iOS"][(i + j) % 4]}, token=tok)
            devs.append(d)
        accounts.append({"phone": phone, "token": tok, "devices": devs})

    a = accounts[0]
    controller = a["devices"][0]
    targets = a["devices"][1:5]  # requested / authorized / active / closed

    def request_session(target):
        return call("/v1/connect/request", {"device_code": target["device_code"], "direct": True},
                    token=a["token"], dtoken=controller["device_token"])

    requested = request_session(targets[0])

    authorized = request_session(targets[1])
    call("/v1/connect/consent", {"session_id": authorized["session_id"], "accepted": True},
         dtoken=targets[1]["device_token"])

    active = request_session(targets[2])
    call("/v1/connect/consent", {"session_id": active["session_id"], "accepted": True},
         dtoken=targets[2]["device_token"])
    call("/v1/sessions/ready", {"session_id": active["session_id"]}, dtoken=controller["device_token"])
    call("/v1/sessions/ready", {"session_id": active["session_id"]}, dtoken=targets[2]["device_token"])

    closed = request_session(targets[3])
    call("/v1/connect/consent", {"session_id": closed["session_id"], "accepted": True},
         dtoken=targets[3]["device_token"])
    call("/v1/sessions/close", {"session_id": closed["session_id"], "reason": "fixture end"},
         dtoken=targets[3]["device_token"])

    return {
        "accounts": len(accounts),
        "devices": sum(len(x["devices"]) for x in accounts),
        "session_ids": {
            "requested": requested["session_id"],
            "authorized": authorized["session_id"],
            "active": active["session_id"],
            "closed": closed["session_id"],
        },
        "controller_device": controller["device_code"],
    }


def main():
    os.makedirs(OUT, exist_ok=True)
    build_stack()
    build_web()
    st = subprocess.Popen([BIN, "-addr", ":8299"],
                          stdout=open("/tmp/er-admui00-stack.log", "w"), stderr=subprocess.STDOUT)
    try:
        for _ in range(80):
            try:
                call("/healthz")
                break
            except Exception:
                time.sleep(0.2)
        fixture = seed()
        admin = call("/admin/v1/login", {"username": ADMIN_USER, "password": ADMIN_PASS, "totp": totp()})
        token = admin["token"]
        users = call("/admin/v1/users", token=token)
        if users["items"]:
            call("/admin/v1/users/ban", {"account_id": users["items"][-1]["id"], "reason": "基线夹具：验证审计写入"}, token=token)

        # 夹具自检：四种会话状态必须真的存在，不能靠截图"看起来有"。
        sess = call("/admin/v1/sessions", token=token)
        observed = sorted({row["state"] for row in sess["items"]})
        want = ["active", "authorized", "closed", "requested"]
        if observed != want:
            print("FIXTURE FAIL: 会话状态 %s，期望 %s" % (observed, want))
            return 1
        fixture["sessions"] = len(sess["items"])
        fixture["sessionStates"] = observed

        class H(http.server.SimpleHTTPRequestHandler):
            def __init__(self, *a, **kw):
                super().__init__(*a, directory=DIST, **kw)

            def log_message(self, *a):
                pass

        class TS(socketserver.ThreadingTCPServer):
            allow_reuse_address = True

        srv = TS(("127.0.0.1", SPORT), functools.partial(H))
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
        errors = []
        screens = []
        with sync_playwright() as p:
            br = p.chromium.launch(channel="chrome", headless=True)

            def new_ctx(with_token=True):
                ctx = br.new_context(viewport=VIEWPORT, device_scale_factor=1, locale="zh-CN")
                ident = json.dumps({"username": admin["username"], "role": admin["role"],
                                    "permissions": admin["permissions"]})
                ctx.add_init_script(
                    "sessionStorage.setItem('er.admin.token', %s);"
                    "sessionStorage.setItem('er_admin_identity', %s);"
                    % (json.dumps(token) if with_token else "''",
                       json.dumps(ident) if with_token else "''"))
                ctx.route("**/admin/v1/**", forward)
                return ctx

            def shot(page, name, title, criteria):
                file = name + ".png"
                page.screenshot(path=os.path.join(OUT, file), full_page=False)
                screens.append({"name": name, "file": file, "title": title, "criteria": criteria})
                print("shot", name, json.dumps(criteria, ensure_ascii=False))

            ctx = new_ctx(with_token=False)
            page = ctx.new_page()
            page.on("pageerror", lambda e: errors.append("a1: " + str(e)))
            page.goto("http://127.0.0.1:%d/" % SPORT, wait_until="networkidle")
            page.wait_for_selector("#login-username", timeout=15000)
            shot(page, "a1-login", "A1 登录（TOTP）", {
                "form": page.locator("form").count(),
                "inputs": page.locator("form input").count(),
                "submitLabel": page.locator("form button[type=submit]").first.inner_text(),
            })
            ctx.close()

            ctx = new_ctx()
            page = ctx.new_page()
            page.on("pageerror", lambda e: errors.append("main: " + str(e)))
            page.goto("http://127.0.0.1:%d/" % SPORT, wait_until="networkidle")
            page.wait_for_selector(".metrics .metric", timeout=15000)

            font = page.evaluate("getComputedStyle(document.body).fontFamily")
            nav = page.locator("nav .nav").all_inner_texts()

            shot(page, "a2-dashboard", "A2 仪表盘", {
                "metricCards": page.locator(".metrics .metric").count(),
                "metricLabels": page.locator(".metrics .metric span").all_inner_texts(),
                "tables": page.locator("table[data-testid=data-table], table.tbl").count(),
                "nav": nav,
            })

            page.locator("nav .nav", has_text="用户").first.click()
            page.wait_for_selector("table[data-testid=data-table] tbody tr", timeout=15000)
            shot(page, "a3-users", "A3 用户列表", {
                "columns": page.locator("table[data-testid=data-table] thead th").all_inner_texts(),
                "rows": page.locator("table[data-testid=data-table] tbody tr").count(),
                "filters": page.locator("[data-card] select, [data-card] input").count(),
                "pager": page.locator(".pager").inner_text(),
            })

            # A6 设备详情：挑一台真的参与过会话的设备（主控机），否则"最近会话"永远是空的。
            page.locator("nav .nav", has_text="设备").first.click()
            page.wait_for_selector("table[data-testid=data-table] tbody tr.cursor-pointer", timeout=15000)
            code = fixture["controller_device"].replace(" ", "")
            shown = code[:3] + " " + code[3:6] + " " + code[6:]
            target_row = page.locator("table[data-testid=data-table] tbody tr.cursor-pointer", has_text=shown).first
            if target_row.count() == 0:
                print("DEVICE ROW NOT FOUND", shown,
                      page.locator("table[data-testid=data-table] tbody tr.cursor-pointer").all_inner_texts()[:3])
                return 1
            target_row.click()
            # A6 已从 modal 迁到独立路由（ADM-UI-06a）：直接量页面。
            page.wait_for_selector("[data-page=device-detail] .kv", timeout=15000)
            page.wait_for_selector('table[aria-label="设备最近会话"] tbody tr', timeout=15000)
            shot(page, "a6-device-detail", "A6 设备详情（独立路由）", {
                "modalTitle": page.locator("[data-page=device-detail] h3").first.inner_text(),
                "kvPairs": page.locator("[data-page=device-detail] .kv span").count(),
                "sessionRows": page.locator('table[aria-label="设备最近会话"] tbody tr').count(),
            })
            page.locator("button", has_text="返回列表").first.click()

            page.locator("nav .nav", has_text="会话").first.click()
            page.wait_for_selector("table[data-testid=data-table] tbody tr", timeout=15000)
            shot(page, "a7-sessions", "A7 会话列表", {
                "columns": page.locator("table[data-testid=data-table] thead th").all_inner_texts(),
                "rows": page.locator("table[data-testid=data-table] tbody tr").count(),
                "states": page.locator("table[data-testid=data-table] tbody tr td:nth-child(4)").all_inner_texts(),
            })

            # A12 配置中心：等配置表真的渲染出来（能力台账是静态表，不能当加载完成的信号）。
            page.locator("nav .nav", has_text="配置中心").first.click()
            page.wait_for_selector("input.cfg-input", timeout=15000)
            shot(page, "a12-config", "A12 配置中心", {
                "columns": page.locator("table").first.locator("thead th").all_inner_texts(),
                "configRows": page.locator("table").first.locator("tbody tr").count(),
                "capabilityRows": page.locator("table").nth(1).locator("tbody tr").count(),
            })

            br.close()
        srv.shutdown()

        manifest = {
            "generatedAt": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
            "viewport": VIEWPORT,
            "deviceScaleFactor": 1,
            "font": font,
            "theme": "light",
            "locale": "zh-CN",
            "baseUrl": "http://127.0.0.1:%d/" % SPORT,
            "fixture": {
                "stack": "server/cmd/stack (内存存储, 127.0.0.1:8299)",
                "accounts": fixture["accounts"],
                "devices": fixture["devices"],
                "sessions": fixture["sessions"],
                "sessionStates": fixture["sessionStates"],
                "sessionIds": fixture["session_ids"],
                "controllerDevice": fixture["controller_device"],
                "admin": {"username": admin["username"], "role": admin["role"],
                          "permissions": admin["permissions"]},
                "note": "开发账号 + 合成手机号；不含生产数据；未设置 ER_ADMIN_TOTP_OFF，走真实 TOTP",
            },
            "screens": screens,
            "pageErrors": errors,
        }
        with open(os.path.join(OUT, "manifest.json"), "w", encoding="utf-8") as f:
            json.dump(manifest, f, ensure_ascii=False, indent=2)
            f.write("\n")
        if errors:
            print("PAGE ERRORS:", errors)
            print("BASELINE FAILED")
            return 1

        if COMPARE_TO:
            # 逐屏比对 DOM 判据：迁移不允许悄悄改变列、行数、标签与导航结构。
            with open(COMPARE_TO, encoding="utf-8") as f:
                base = json.load(f)

            def norm(key, value):
                # 设备码是随机生成的，弹窗标题里的数字不参与比对。
                return re.sub(r"\d", "#", str(value)) if key == "modalTitle" else value

            diffs = []
            for shot in base.get("screens", []):
                fresh = next((s for s in screens if s["name"] == shot["name"]), None)
                if fresh is None:
                    diffs.append("%s: 缺少截图" % shot["name"])
                    continue
                keys = set(shot.get("criteria", {})) | set(fresh.get("criteria", {}))
                for key in sorted(keys):
                    a = norm(key, shot.get("criteria", {}).get(key))
                    b = norm(key, fresh.get("criteria", {}).get(key))
                    if a != b:
                        diffs.append("%s.%s: %r -> %r" % (shot["name"], key, a, b))
            report = {"comparedTo": COMPARE_TO, "diffs": diffs, "screens": len(base.get("screens", []))}
            with open(os.path.join(OUT, "compare.json"), "w", encoding="utf-8") as f:
                json.dump(report, f, ensure_ascii=False, indent=2)
                f.write("\n")
            if diffs:
                print("VISUAL DIFF:")
                for d in diffs:
                    print("  -", d)
                print("BASELINE COMPARE FAILED")
                return 1
            print("BASELINE COMPARE OK (%d 屏 DOM 判据一致)" % report["screens"])

        print("screens:", ", ".join(s["name"] for s in screens))
        print("font:", font)
        print("BASELINE OK")
        return 0
    finally:
        st.terminate()
        try:
            st.wait(timeout=5)
        except subprocess.TimeoutExpired:
            st.kill()


if __name__ == "__main__":
    sys.exit(main())
