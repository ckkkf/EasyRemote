#!/usr/bin/env python3
"""ADM-UI-04 E2E：仪表盘 5s 轮询、暂停、陈旧标记、缺接口指标显示「数据未提供」。

真实 stack + 真实 admin API + 真实构建产物。
输出 docs/impl/evidence/ADM-UI-04/e2e-dashboard.json + 截图，末行打印 E2E OK。
"""
import base64, functools, hashlib, hmac, http.server, json, os, socketserver, struct, subprocess, sys, threading, time, urllib.error, urllib.request
from admin_e2e import pick

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BIN = "/tmp/er-admui04-stack"
STACK = "http://127.0.0.1:8299"
SPORT = 5397
DIST = os.path.join(ROOT, "web-admin", "dist")
OUT = os.path.join(ROOT, "docs/impl/evidence/ADM-UI-04")
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
    try:
        with urllib.request.urlopen(req) as resp:
            return json.loads(resp.read().decode()).get("data", {})
    except urllib.error.HTTPError as e:
        raise RuntimeError("%s %s -> %s %s" % (m, path, e.code, e.read().decode()[:200])) from e


def totp(secret=TOTP_SECRET):
    s = secret + "=" * ((8 - len(secret) % 8) % 8)
    dig = hmac.new(base64.b32decode(s), struct.pack(">Q", int(time.time()) // 30), hashlib.sha256).digest()
    off = dig[-1] & 0x0F
    v = (dig[off] & 0x7F) << 24 | dig[off + 1] << 16 | dig[off + 2] << 8 | dig[off + 3]
    return str(v % 1000000).zfill(6)


def seed():
    devices = 0
    controller = None
    for i in range(3):
        phone = "139000040%02d" % i
        call("/v1/auth/sms", {"phone": phone})
        tok = call("/v1/auth/login", {"phone": phone, "code": "123456"})["token"]
        for p in (["macOS", "Windows"] if i == 0 else ["Android"]):
            d = call("/v1/devices/bind", {"platform": p}, token=tok)
            devices += 1
            if controller is None:
                controller = {"token": tok, "device": d}
    # 造一条会话并上报 3 秒质量采样（ADM-API-02e 的真实数据源）。
    caller = controller
    # 设备码随机，列表按 code 排序；必须挑"不是主控自己"的那台，否则会触发自连接。
    mine = call("/v1/devices", token=caller["token"])["devices"]
    ctrl_code = caller["device"]["raw_code"]
    target = next(d for d in mine if d["raw_code"] != ctrl_code)
    sess = call("/v1/connect/request", {"device_code": target["raw_code"], "direct": True},
                token=caller["token"], dtoken=caller["device"]["device_token"])
    now = int(time.time())
    call("/v1/sessions/quality", {
        "session_id": sess["session_id"],
        "samples": [
            {"ts": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(now - 2)), "latency_ms": 28,
             "loss_permille": 1, "fps": 60, "relay": False, "link": "p2p", "target_fps": 60},
            {"ts": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(now - 1)), "latency_ms": 34,
             "loss_permille": 2, "fps": 59, "relay": True, "link": "relay", "target_fps": 60},
            {"ts": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(now)), "latency_ms": 41,
             "loss_permille": 3, "fps": 58, "relay": True, "link": "relay", "target_fps": 60},
        ],
    }, dtoken=caller["device"]["device_token"])
    # ADM-API-02f5：上报一次崩溃与两次重连（一次成功），供稳定性组最后两项。
    for kind in ("crash", "reconnect_attempt", "reconnect_attempt", "reconnect_success"):
        call("/v1/sessions/health", {"session_id": sess["session_id"], "kind": kind},
             dtoken=caller["device"]["device_token"])
    # ADM-API-02f1：再结束一次会话（网络中断），用于验证异常中断率来自真实结束原因。
    call("/v1/sessions/close", {"session_id": sess["session_id"], "reason": "network changed"},
         token=caller["token"], dtoken=caller["device"]["device_token"])
    return devices


def main():
    os.makedirs(OUT, exist_ok=True)
    subprocess.run(["go", "build", "-o", BIN, "./cmd/stack"], cwd=os.path.join(ROOT, "server"), check=True)
    r = subprocess.run(["npm", "run", "build"], cwd=os.path.join(ROOT, "web-admin"),
                       stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    if r.returncode != 0:
        print(r.stdout)
        return 1

    st = subprocess.Popen([BIN, "-addr", ":8299"], stdout=open("/tmp/er-admui04-stack.log", "w"),
                          stderr=subprocess.STDOUT)
    try:
        for _ in range(80):
            try:
                call("/healthz")
                break
            except Exception:
                time.sleep(0.2)
        devices = seed()
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

        counters = {"overview": 0}

        def forward(route):
            req = route.request
            if "/admin/v1/stats/overview" in req.url:
                counters["overview"] += 1
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

            page.goto(base + "/dashboard", wait_until="networkidle")
            page.wait_for_selector("[data-group=scale]", timeout=15000)

            check("three metric groups rendered",
                  all(page.locator("[data-group=%s]" % g).count() == 1 for g in ("scale", "quality", "stability")))
            check("scale shows authoritative values",
                  page.locator("[data-metric=devices_total] strong").inner_text() == str(devices),
                  "%s vs fixture %d" % (page.locator("[data-metric=devices_total] strong").inner_text(), devices))
            # ADM-API-02a：在线用户由服务端按账号去重给出真值（夹具 3 个账号）。
            check("users_online shows authoritative value",
                  page.locator("[data-metric=users_online] strong").inner_text() == "3",
                  page.locator("[data-metric=users_online] strong").inner_text())
            # ADM-API-02f6：24h 新增设备按设备绑定时间统计（本例 4 台全是刚绑定）。
            check("devices new 24h from device created_at",
                  page.locator("[data-metric=devices_new_24h] strong").inner_text() == "4",
                  page.locator("[data-metric=devices_new_24h] strong").inner_text())
            # ADM-API-02e：有采样时 P50/P95/中继占比是真值，其余仍「数据未提供」。
            check("quality p50 from samples",
                  page.locator("[data-metric=latency_p50] strong").inner_text() == "34 ms",
                  page.locator("[data-metric=latency_p50] strong").inner_text())
            check("quality p95 from samples",
                  page.locator("[data-metric=latency_p95] strong").inner_text() == "41 ms",
                  page.locator("[data-metric=latency_p95] strong").inner_text())
            check("quality relay percent from samples",
                  page.locator("[data-metric=relay_ratio] strong").inner_text() == "66%",
                  page.locator("[data-metric=relay_ratio] strong").inner_text())
            # ADM-API-02f2：建连成功率来自真实尝试（本例 1 次尝试、0 次 active → 0%）。
            check("connect success rate from real attempts",
                  page.locator("[data-metric=connect_success_rate] strong").inner_text() == "0%",
                  page.locator("[data-metric=connect_success_rate] strong").inner_text())
            check("connect attempts count shown",
                  "尝试 1 次" in page.locator("[data-metric=connect_success_rate]").inner_text())
            # ADM-API-02f4：P2P 命中率与帧率达成率来自客户端上报的 link/target_fps。
            check("p2p hit rate from reported link",
                  page.locator("[data-metric=p2p_hit_rate] strong").inner_text() == "33%",
                  page.locator("[data-metric=p2p_hit_rate] strong").inner_text())
            check("fps target rate from reported target",
                  page.locator("[data-metric=fps_target_rate] strong").inner_text() == "33%",
                  page.locator("[data-metric=fps_target_rate] strong").inner_text())
            # ADM-API-02f1：异常中断率 = 异常结束 / 结束会话（本例 1/1 = 100%）。
            check("abnormal close rate from real reasons",
                  page.locator("[data-metric=abnormal_close_rate] strong").inner_text() == "100%",
                  page.locator("[data-metric=abnormal_close_rate] strong").inner_text())
            check("stability subtitle shows closed count",
                  "结束会话 1 次" in page.locator("[data-group=stability]").inner_text())
            # ADM-API-02f5：崩溃率 = 崩溃/窗口内会话数（1/1）；重连成功率 = 1/2。
            check("crash rate from reported events",
                  page.locator("[data-metric=client_crash_rate] strong").inner_text() == "100%",
                  page.locator("[data-metric=client_crash_rate] strong").inner_text())
            check("reconnect success rate from reported events",
                  page.locator("[data-metric=reconnect_success_rate] strong").inner_text() == "50%",
                  page.locator("[data-metric=reconnect_success_rate] strong").inner_text())
            # ADM-API-02f3：趋势来自真实时间桶（本例 1 个桶：当前小时）。
            trend_sub = page.locator("text=个时间桶").first.inner_text()
            check("trend buckets rendered", "1 个时间桶" in trend_sub, trend_sub)
            pts = page.locator('svg[aria-label="趋势 会话开始"] polyline').get_attribute("points")
            check("trend polyline has points", bool(pts) and len(pts.split(" ")) == 1, pts)
            check("trend shows peak", "峰值 1" in page.locator("body").inner_text())
            check("updated-at rendered", "更新于" in page.locator("body").inner_text())
            page.screenshot(path=os.path.join(OUT, "dashboard.png"))

            # 统计窗口切换要真的带 range 参数请求服务端。
            with page.expect_response(lambda r: "/admin/v1/stats/overview" in r.url and "range=7d" in r.url,
                                      timeout=15000):
                pick(page, "质量统计窗口", "7d")
            check("range switch hits server with range param", True)
            pick(page, "质量统计窗口", "24h")
            page.wait_for_timeout(500)

            # 5s 轮询：等 6.5s 至少再请求一次
            first = counters["overview"]
            page.wait_for_timeout(6500)
            polled = counters["overview"]
            check("polls every 5s", polled >= first + 1, "%d -> %d" % (first, polled))

            # 暂停后不再请求
            page.get_by_role("switch", name="自动刷新").first.click()
            paused_at = counters["overview"]
            page.wait_for_timeout(6500)
            check("pause stops polling", counters["overview"] == paused_at, "%d -> %d" % (paused_at, counters["overview"]))

            # 暂停 15s 后出现陈旧标记
            page.wait_for_selector("[data-testid=stale-badge]", timeout=20000)
            check("stale badge appears when paused", page.locator("[data-testid=stale-badge]").is_visible())
            page.screenshot(path=os.path.join(OUT, "dashboard-stale.png"))

            # 恢复自动刷新：重新请求且陈旧标记消失
            page.get_by_role("switch", name="自动刷新").first.click()
            page.wait_for_timeout(6000)
            check("resume polls again", counters["overview"] > paused_at, "%d -> %d" % (paused_at, counters["overview"]))
            check("stale badge clears after refresh",
                  page.locator("[data-testid=stale-badge]").count() == 0)

            br.close()
        srv.shutdown()

        result = {"generatedAt": time.strftime("%Y-%m-%dT%H:%M:%S%z"), "viewport": VIEWPORT,
                  "checks": checks, "pageErrors": errors, "overviewRequests": counters["overview"]}
        with open(os.path.join(OUT, "e2e-dashboard.json"), "w", encoding="utf-8") as f:
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
