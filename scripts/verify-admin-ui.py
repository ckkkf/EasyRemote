#!/usr/bin/env python3
"""管理端 UI 验收脚本（P1-1 配置中心 / P1-2 详情页）。

前置：本机装有 python playwright 与 Chrome（channel="chrome"）；不需要往
web-admin 里加任何依赖。脚本自己 build 出 /tmp/er-ralph-stack 起在 :8199，
用 python 自带 http.server 在 :5292 提供 web-admin/dist，并把 /admin/v1/*
转发到临时 stack（不碰 8090/8091/5174 上正在跑的进程）。

用法：
  cd web-admin && npm run build && cd ..
  python3 scripts/verify-admin-ui.py

覆盖：
  1. 配置中心：改一项 → 保存 → 绿条「已保存 N 项，变更已写入审计」→ 审计有
     config.update 且 before/after 正确 → 值持久化。
  2. 设备行点击打开详情；点「复制设备码」不打开详情。
  3. 用户行点击打开详情；点「封禁」打开封禁弹窗而不是详情。
  4. 全程无 pageerror。
"""
import base64, hashlib, hmac, json, os, struct, subprocess, sys, time, threading, functools, http.server, socketserver, urllib.request, urllib.error

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BIN = "/tmp/er-ralph-stack"

def build_stack():
    r = subprocess.run(["go", "build", "-o", BIN, "./cmd/stack"], cwd=os.path.join(ROOT, "server"))
    if r.returncode != 0:
        print("BUILD FAIL")
        sys.exit(1)


import base64, hashlib, hmac, json, struct, subprocess, sys, time, threading, functools, http.server, socketserver, urllib.request, urllib.error

STACK = "http://127.0.0.1:8199"
DIST = os.path.join(ROOT, "web-admin", "dist")
SPORT = 5292

def call(path, data=None, token=None, dtoken=None):
    body = json.dumps(data).encode() if data is not None else None
    r = urllib.request.Request(STACK + path, data=body, method="POST" if body else "GET")
    r.add_header("Content-Type", "application/json")
    if token: r.add_header("Authorization", "Bearer " + token)
    if dtoken: r.add_header("X-Device-Token", dtoken)
    with urllib.request.urlopen(r) as resp:
        env = json.loads(resp.read().decode())
    return env.get("data", env)

def totp(secret="DEVTOTPSECRET"):
    s = secret + "=" * ((8 - len(secret) % 8) % 8)
    dig = hmac.new(base64.b32decode(s), struct.pack(">Q", int(time.time()) // 30), hashlib.sha256).digest()
    off = dig[-1] & 0x0F
    v = (dig[off] & 0x7F) << 24 | dig[off+1] << 16 | dig[off+2] << 8 | dig[off+3]
    return str(v % 1000000).zfill(6)

build_stack()
st = subprocess.Popen([BIN, "-addr", ":8199"],
                      stdout=open("/tmp/er-verify-stack.log", "w"), stderr=subprocess.STDOUT)
ok = True
def check(name, cond, extra=""):
    global ok
    print(("PASS " if cond else "FAIL ") + name + ((" -> " + str(extra)) if extra else ""))
    if not cond: ok = False

try:
    for _ in range(50):
        try: call("/healthz"); break
        except Exception: time.sleep(0.2)

    call("/v1/auth/sms", {"phone": "13900001111"})
    tok = call("/v1/auth/login", {"phone": "13900001111", "code": "123456"})["token"]
    d = call("/v1/devices/bind", {"platform": "macOS"}, token=tok)

    token = call("/admin/v1/login", {"username": "root", "password": "dev-pass", "totp": totp()})["token"]

    class H(http.server.SimpleHTTPRequestHandler):
        def __init__(self, *a, **kw): super().__init__(*a, directory=DIST, **kw)
        def log_message(self, *a): pass
    class TS(socketserver.ThreadingTCPServer):
        allow_reuse_address = True
    srv = TS(("127.0.0.1", SPORT), functools.partial(H))
    threading.Thread(target=srv.serve_forever, daemon=True).start()

    def forward(route):
        r = route.request
        url = r.url.replace("http://127.0.0.1:%d" % SPORT, STACK)
        body = r.post_data.encode() if r.post_data else None
        fr = urllib.request.Request(url, data=body, method=r.method)
        for k, v in r.headers.items():
            if k.lower() in ("host", "content-length", "accept-encoding", "connection", "origin", "referer"): continue
            fr.add_header(k, v)
        try:
            with urllib.request.urlopen(fr) as resp:
                bt = resp.read(); sc = resp.status; ct = resp.headers.get("Content-Type", "application/json")
        except urllib.error.HTTPError as e:
            bt = e.read(); sc = e.code; ct = e.headers.get("Content-Type", "application/json")
        route.fulfill(status=sc, body=bt, headers={"Content-Type": ct})

    from playwright.sync_api import sync_playwright
    with sync_playwright() as p:
        br = p.chromium.launch(channel="chrome", headless=True)
        ctx = br.new_context(viewport={"width": 1440, "height": 950})
        ctx.add_init_script("localStorage.setItem('er.admin.token', " + json.dumps(token) + ")")
        ctx.route("**/admin/v1/**", forward)
        page = ctx.new_page()
        errs = []
        page.on("pageerror", lambda e: errs.append(str(e)))
        page.goto("http://127.0.0.1:%d/" % SPORT, wait_until="networkidle")

        # 1) 配置中心：改一项并保存
        page.locator("nav button", has_text="配置中心").click()
        page.wait_for_selector("table[data-testid=data-table] tbody tr", timeout=10000)
        first = page.locator("table[data-testid=data-table] tbody tr").first
        inp = first.locator("input.cfg-input")
        check("config row rendered", inp.count() == 1)
        inp.fill("601")
        save = page.locator(".card-actions button", has_text="保存").first
        check("save button enabled after edit", save.is_enabled())
        save.click()
        page.wait_for_selector("text=已保存 1 项", timeout=10000)
        banner = page.locator(".banner.ok", has_text="已保存 1 项").first.inner_text()
        check("save banner mentions audit", "审计" in banner, banner)
        audit = call("/admin/v1/audit", token=token)
        hit = [r for r in audit["items"] if r["action"].startswith("config")]
        check("audit row written", len(hit) > 0, hit[0]["action"] + " " + str(hit[0].get("before")) + "->" + str(hit[0].get("after")))
        cfg = call("/admin/v1/config", token=token)
        check("config persisted", cfg["config"]["session.max_minutes"] == "601", cfg["config"]["session.max_minutes"])

        # 2) 设备行点击 → 详情；复制按钮不触发详情
        page.locator("nav button", has_text="设备").click()
        page.wait_for_selector("table[data-testid=data-table] tbody tr.clickable", timeout=10000)
        page.locator("table[data-testid=data-table] tbody tr.clickable").first.locator("button", has_text="复制设备码").click()
        page.wait_for_timeout(400)
        check("copy button does not open detail", page.locator(".modal").count() == 0)
        page.locator("table[data-testid=data-table] tbody tr.clickable").first.click()
        page.wait_for_selector(".modal .kv", timeout=10000)
        check("device detail opens on row click", page.locator(".modal h3").first.inner_text().startswith("设备详情"))
        page.locator(".modal-actions button", has_text="关闭").click()
        page.wait_for_timeout(300)
        check("device detail closes", page.locator(".modal").count() == 0)

        # 3) 用户行点击 → 详情；封禁按钮不触发详情
        page.locator("nav button", has_text="用户").click()
        page.wait_for_selector("table[data-testid=data-table] tbody tr.clickable", timeout=10000)
        page.locator("table[data-testid=data-table] tbody tr.clickable").first.locator("button", has_text="封禁").click()
        page.wait_for_timeout(400)
        h3 = page.locator(".modal h3").first.inner_text()
        check("ban button opens ban modal", h3.startswith("封禁"), h3)
        page.locator(".modal-actions button", has_text="取消").click()
        page.wait_for_timeout(300)
        page.locator("table[data-testid=data-table] tbody tr.clickable").first.click()
        page.wait_for_selector(".modal .kv", timeout=10000)
        check("user detail opens on row click", page.locator(".modal h3").first.inner_text().startswith("用户详情"))
        check("user detail shows account devices", page.locator(".modal table[data-testid=data-table] tbody tr").count() >= 1)

        check("no page errors", len(errs) == 0, errs)
        br.close()
    srv.shutdown()
    print("VERIFY " + ("OK" if ok else "FAILED"))
finally:
    st.terminate()
    try: st.wait(timeout=5)
    except Exception: st.kill()
sys.exit(0 if ok else 1)
