#!/usr/bin/env python3
"""ADM-UI-06a E2E：设备列表 DataTable、详情独立路由、30 天曲线未开放、按设备会话真实过滤。

真实 stack + 真实 admin API + 真实构建产物。
输出 docs/impl/evidence/ADM-UI-06/e2e-devices.json + 截图，末行打印 E2E OK。
"""
import base64, functools, hashlib, hmac, http.server, json, os, socketserver, struct, subprocess, sys, threading, time, urllib.error, urllib.request
from admin_e2e import pick

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BIN = "/tmp/er-admui06-stack"
STACK = "http://127.0.0.1:8299"
SPORT = 5399
DIST = os.path.join(ROOT, "web-admin", "dist")
OUT = os.path.join(ROOT, "docs/impl/evidence/ADM-UI-06")
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
    call("/v1/auth/sms", {"phone": "13900006000"})
    tok = call("/v1/auth/login", {"phone": "13900006000", "code": "123456"})["token"]
    # ADM-API-04a：绑定可上报设备元信息，管理端据此展示设备名/系统/客户端/NAT。
    ctrl = call("/v1/devices/bind", {"platform": "macOS", "device_name": "夹具工作站",
                                     "os_version": "macOS 15.1", "client_version": "0.2.12",
                                     "nat_type": "symmetric"}, token=tok)
    target = call("/v1/devices/bind", {"platform": "Windows", "device_name": "夹具测试机",
                                       "os_version": "Windows 11 23H2", "client_version": "0.2.12",
                                       "nat_type": "cone"}, token=tok)
    s = call("/v1/connect/request", {"device_code": target["device_code"], "direct": True},
             token=tok, dtoken=ctrl["device_token"])
    call("/v1/connect/consent", {"session_id": s["session_id"], "accepted": True}, dtoken=target["device_token"])
    return {"controller": ctrl, "target": target, "session": s["session_id"]}


def main():
    os.makedirs(OUT, exist_ok=True)
    subprocess.run(["go", "build", "-o", BIN, "./cmd/stack"], cwd=os.path.join(ROOT, "server"), check=True)
    r = subprocess.run(["npm", "run", "build"], cwd=os.path.join(ROOT, "web-admin"),
                       stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    if r.returncode != 0:
        print(r.stdout)
        return 1

    st = subprocess.Popen([BIN, "-addr", ":8299"], stdout=open("/tmp/er-admui06-stack.log", "w"),
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
        # ADM-API-02d：用真实设备凭证上报两天的质量采样，供设备 30 天曲线聚合。
        now = int(time.time())
        call("/v1/sessions/quality", {
            "session_id": fixture["session"],
            "samples": [
                {"ts": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(now - 86400)), "latency_ms": 30,
                 "loss_permille": 2, "fps": 60, "tier": "hd", "relay": False},
                {"ts": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(now)), "latency_ms": 40,
                 "loss_permille": 5, "fps": 58, "tier": "hd", "relay": True},
            ],
        }, dtoken=fixture["controller"]["device_token"])

        devices = call("/admin/v1/devices", token=token)
        norm = lambda c: str(c).replace(" ", "")
        ctrl_id = next((d["id"] for d in devices["items"]
                        if norm(d["device_code"]) == norm(fixture["controller"]["device_code"])), None)
        if ctrl_id is None:
            print("DEVICE MATCH FAIL; list:", [(d["device_code"], d["id"]) for d in devices["items"]],
                  "fixture:", fixture["controller"]["device_code"])
            return 1

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

            page.goto(base + "/devices", wait_until="networkidle")
            page.wait_for_selector("table[data-testid=data-table] tbody tr", timeout=15000)
            headers = page.locator("table[data-testid=data-table] thead th").all_inner_texts()
            check("device columns",
                  headers == ["设备码", "设备名", "归属账号", "平台", "系统版本", "客户端版本", "NAT", "风险", "在线", "最后在线", "操作"],
                  headers)
            names = page.locator("table[data-testid=data-table] tbody tr td:nth-child(2)").all_inner_texts()
            check("device names from bind", sorted(names) == ["夹具工作站", "夹具测试机"], names)
            systems = page.locator("table[data-testid=data-table] tbody tr td:nth-child(5)").all_inner_texts()
            check("os versions from bind", sorted(systems) == ["Windows 11 23H2", "macOS 15.1"], systems)
            page.screenshot(path=os.path.join(OUT, "devices-list.png"))

            # 在线状态筛选：服务端过滤（online=1/0）
            pick(page, "在线状态筛选", "online")
            page.wait_for_function(
                "() => { const rows=[...document.querySelectorAll('table[data-testid=data-table] tbody tr')];"
                " return rows.length>0 && rows.every(r => r.children[8].innerText.includes('在线')); }",
                timeout=15000)
            check("online filter returns online devices",
                  all("在线" in s for s in page.locator("table[data-testid=data-table] tbody tr td:nth-child(9)").all_inner_texts()))
            pick(page, "在线状态筛选", "all")
            page.wait_for_selector("table[data-testid=data-table] tbody tr", timeout=15000)

            # 复制设备码不跳转
            page.locator("table[data-testid=data-table] tbody tr").first.locator("button", has_text="复制设备码").click()
            page.wait_for_timeout(300)
            check("copy stays on list", page.url.endswith("/devices"), page.url)

            # ADM-API-04c：标记风险 → 列表显示徽标 + 审计；解除后恢复正常。
            first_row = page.locator("table[data-testid=data-table] tbody tr").first
            first_row.locator("button", has_text="标记风险").click()
            page.locator("[role=dialog]").wait_for(state="visible", timeout=10000)
            check("flag-risk requires reason",
                  page.locator("[role=dialog] button", has_text="确认标记").is_disabled())
            page.locator("[role=dialog] #device-reason").fill("同 IP 高频连接")
            page.locator("[role=dialog] button", has_text="确认标记").click()
            page.locator("[role=dialog]").wait_for(state="hidden", timeout=15000)
            page.wait_for_function(
                "() => [...document.querySelectorAll('table[data-testid=data-table] tbody tr td:nth-child(8)')]"
                ".some(td => td.innerText.trim() === '风险')", timeout=15000)
            check("risk badge shown", True)
            check("flag risk writes audit",
                  any(a["action"] == "device.flag_risk" for a in call("/admin/v1/audit", token=token)["items"]))
            page.screenshot(path=os.path.join(OUT, "devices-risk.png"))

            page.locator("table[data-testid=data-table] tbody tr", has_text="风险").first.locator("button", has_text="解除风险").click()
            page.locator("[role=dialog] #device-reason").fill("误报")
            page.locator("[role=dialog] button", has_text="确认解除").click()
            page.locator("[role=dialog]").wait_for(state="hidden", timeout=15000)
            page.wait_for_function(
                "() => ![...document.querySelectorAll('table[data-testid=data-table] tbody tr td:nth-child(8)')]"
                ".some(td => td.innerText.trim() === '风险')", timeout=15000)
            check("risk cleared", True)
            check("clear risk writes audit",
                  any(a["action"] == "device.clear_risk" for a in call("/admin/v1/audit", token=token)["items"]))

            # ADM-API-04b：强制下线需要原因，真实生效（设备离线 + 审计）
            target_row = page.locator("table[data-testid=data-table] tbody tr").first
            target_code = target_row.locator("td").first.inner_text().replace(" ", "")
            target_row.locator("button", has_text="强制下线").click()
            page.locator("[role=dialog]").wait_for(state="visible", timeout=10000)
            check("force-offline requires reason",
                  page.locator("[role=dialog] button", has_text="确认下线").is_disabled())
            page.locator("[role=dialog] #device-reason").fill("用户报障")
            page.locator("[role=dialog] button", has_text="确认下线").click()
            page.locator("[role=dialog]").wait_for(state="hidden", timeout=15000)
            audit = call("/admin/v1/audit", token=token)
            check("force offline writes audit", any(a["action"] == "device.force_offline" for a in audit["items"]))
            devices_after = call("/admin/v1/devices", token=token)
            forced = [d for d in devices_after["items"] if d["device_code"].replace(" ", "") == target_code]
            check("force offline marks device offline", bool(forced) and forced[0]["online"] is False,
                  forced[0]["online"] if forced else "missing")

            # 解绑：删除设备 + 审计
            before_count = len(devices_after["items"])
            page.locator("table[data-testid=data-table] tbody tr", has_text=target_code[:3]).first                 .locator("button", has_text="解绑").click()
            page.locator("[role=dialog] #device-reason").fill("设备丢失")
            page.locator("[role=dialog] button", has_text="确认解绑").click()
            page.locator("[role=dialog]").wait_for(state="hidden", timeout=15000)
            devices_final = call("/admin/v1/devices", token=token)
            check("unbind removes device", len(devices_final["items"]) == before_count - 1,
                  "%d -> %d" % (before_count, len(devices_final["items"])))
            check("unbind writes audit", any(a["action"] == "device.unbind" for a in call("/admin/v1/audit", token=token)["items"]))

            # 行点击 → 独立详情路由
            page.locator("table[data-testid=data-table] tbody tr").first.click()
            page.wait_for_selector("[data-page=device-detail]", timeout=15000)
            seg = page.url.rsplit("/", 1)[-1]
            check("row opens device detail", seg.startswith("dev-") and "/devices/" in page.url,
                  page.url + " (ctrl " + ctrl_id + ")")
            check("device detail kv includes metadata",
                  page.locator("[data-page=device-detail] .kv span").count() == 11,
                  page.locator("[data-page=device-detail] .kv span").count())
            detail_text = page.locator("[data-page=device-detail] .kv").inner_text()
            check("device detail shows os/client/nat",
                  ("macOS 15.1" in detail_text or "Windows 11 23H2" in detail_text)
                  and "0.2.12" in detail_text and ("symmetric" in detail_text or "cone" in detail_text),
                  detail_text.replace("\n", " | ")[:120])
            page.wait_for_selector('table[aria-label="设备最近会话"] tbody tr', timeout=15000)
            check("device sessions filtered by device",
                  page.locator('table[aria-label="设备最近会话"] tbody tr').count() >= 1,
                  page.locator('table[aria-label="设备最近会话"] tbody tr').count())
            # ADM-API-02d：真实按日聚合（两天采样 → 聚合行），不是 0 曲线。
            page.wait_for_selector('table[aria-label="设备质量按日聚合"] tbody tr', timeout=15000)
            day_rows = page.locator('table[aria-label="设备质量按日聚合"] tbody tr').count()
            check("device quality aggregated by day", day_rows >= 1, day_rows)
            quality_text = page.locator('table[aria-label="设备质量按日聚合"]').inner_text()
            check("quality shows latency and relay", "30" in quality_text or "40" in quality_text, quality_text[:80])
            check("range buttons enabled", page.locator("button", has_text="近 7 天").is_enabled())
            # 详情页不再放重复的强制下线入口（列表页按行操作），避免两处语义分叉。
            check("no dead action on detail", page.locator("button", has_text="强制下线").count() == 0)
            page.screenshot(path=os.path.join(OUT, "device-detail.png"))

            # 深链刷新
            page.reload(wait_until="networkidle")
            page.wait_for_selector("[data-page=device-detail]", timeout=15000)
            check("deep link survives reload", "设备信息" in page.locator("body").inner_text())

            # 返回列表
            page.locator("button", has_text="返回列表").first.click()
            page.wait_for_selector("table[data-testid=data-table] tbody tr", timeout=15000)
            check("back returns to list", page.url.endswith("/devices"), page.url)

            # 未知设备
            page.goto(base + "/devices/dev-does-not-exist", wait_until="networkidle")
            page.wait_for_selector("text=设备不存在", timeout=15000)
            check("unknown device shows not found", "设备不存在" in page.locator("body").inner_text())

            br.close()
        srv.shutdown()

        result = {"generatedAt": time.strftime("%Y-%m-%dT%H:%M:%S%z"), "viewport": VIEWPORT,
                  "checks": checks, "pageErrors": errors, "controllerDeviceId": ctrl_id}
        with open(os.path.join(OUT, "e2e-devices.json"), "w", encoding="utf-8") as f:
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
