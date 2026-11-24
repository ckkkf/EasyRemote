#!/usr/bin/env python3
"""ADM-UI-10 响应式与可访问性审计：4 档宽度 × 7 个页面，真实 stack + 真实构建产物。

检查项（都可执行，不靠"看起来还行"）：
  1. 页面级横向溢出（body.scrollWidth > innerWidth）
  2. 表格溢出策略：表格超出容器时，容器必须可横向滚动（否则就是遮挡）
  3. 交互目标尺寸：按钮/链接高度 >= 24px
  4. 键盘：Tab 能进入导航，focus 元素有可见焦点样式
  5. 对比度：正文文字与背景对比度 >= 4.5
  6. 减少动效：prefers-reduced-motion 下 transition/animation 时长被压到近 0
输出 docs/impl/evidence/ADM-UI-10/audit.json + 截图，末行打印 AUDIT OK。
"""
import base64, functools, hashlib, hmac, http.server, json, os, socketserver, struct, subprocess, sys, threading, time, urllib.error, urllib.request

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BIN = "/tmp/er-admui10-stack"
STACK = "http://127.0.0.1:8299"
SPORT = 5402
DIST = os.path.join(ROOT, "web-admin", "dist")
OUT = os.path.join(ROOT, "docs/impl/evidence/ADM-UI-10")
TOTP_SECRET = os.environ.get("ER_ADMIN_TOTP_SECRET", "DEVTOTPSECRET")
VIEWPORTS = [(1360, 860), (1280, 800), (1024, 768), (768, 1024)]
PAGES = ["/login", "/dashboard", "/users", "/devices", "/sessions", "/audit", "/config"]


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


def seed():
    for i in range(3):
        phone = "139000090%02d" % i
        call("/v1/auth/sms", {"phone": phone})
        tok = call("/v1/auth/login", {"phone": phone, "code": "123456"})["token"]
        for p in (["macOS", "Windows"] if i == 0 else ["Android"]):
            call("/v1/devices/bind", {"platform": p}, token=tok)


def main():
    os.makedirs(OUT, exist_ok=True)
    subprocess.run(["go", "build", "-o", BIN, "./cmd/stack"], cwd=os.path.join(ROOT, "server"), check=True)
    r = subprocess.run(["npm", "run", "build"], cwd=os.path.join(ROOT, "web-admin"),
                       stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    if r.returncode != 0:
        print(r.stdout)
        return 1

    st = subprocess.Popen([BIN, "-addr", ":8299"], stdout=open("/tmp/er-admui10-stack.log", "w"),
                          stderr=subprocess.STDOUT)
    try:
        for _ in range(80):
            try:
                call("/healthz")
                break
            except Exception:
                time.sleep(0.2)
        seed()
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
        findings = []
        shots = []

        def fail(kind, detail):
            findings.append({"kind": kind, "detail": detail})
            print("FAIL", kind, detail)

        with sync_playwright() as p:
            br = p.chromium.launch(channel="chrome", headless=True)
            for (w, h) in VIEWPORTS:
                ctx = br.new_context(viewport={"width": w, "height": h}, locale="zh-CN")
                ident = json.dumps({"username": admin["username"], "role": admin["role"],
                                    "permissions": admin["permissions"]})
                ctx.add_init_script("sessionStorage.setItem('er.admin.token', %s);"
                                    "sessionStorage.setItem('er_admin_identity', %s);"
                                    % (json.dumps(token), json.dumps(ident)))
                ctx.route("**/admin/v1/**", forward)
                page = ctx.new_page()
                for path in PAGES:
                    page.goto("http://127.0.0.1:%d%s" % (SPORT, path), wait_until="networkidle")
                    page.wait_for_timeout(300)
                    name = "%dx%d%s" % (w, h, path.replace("/", "-"))
                    page.screenshot(path=os.path.join(OUT, "shots", name + ".png"), full_page=False)
                    shots.append(name)

                    m = page.evaluate("""() => {
                      const doc = document.documentElement;
                      const overflow = doc.scrollWidth > window.innerWidth + 1;
                      const tables = [...document.querySelectorAll('table[data-testid=data-table]')].map(t => {
                        const wrap = t.parentElement;
                        const cs = wrap ? getComputedStyle(wrap) : null;
                        return {
                          tableW: t.scrollWidth,
                          wrapW: wrap ? wrap.clientWidth : 0,
                          scrollable: !!cs && (cs.overflowX === 'auto' || cs.overflowX === 'scroll'),
                          wrapClass: wrap ? wrap.className : '',
                        };
                      });
                      // 溢出诊断：找出右侧越过视口的元素，失败时打印，省得靠猜。
                      const wide = [...document.querySelectorAll('*')]
                        .filter(el => { const r = el.getBoundingClientRect(); return r.width > 0 && r.right > window.innerWidth + 1; })
                        .slice(0, 3)
                        .map(el => (el.tagName.toLowerCase() + '.' + (el.className || '')).slice(0, 40));
                      const small = [...document.querySelectorAll('button, a.nav')]
                        .filter(el => { const r = el.getBoundingClientRect(); return r.width > 0 && r.height > 0 && r.height < 24; })
                        .map(el => (el.innerText || '').trim().slice(0, 12));
                      return {overflow, tables, small, wide, title: document.title};
                    }""")
                    if m["overflow"]:
                        fail("page-overflow", "%dx%d %s wide=%s" % (w, h, path, m.get("wide")))
                    for t in m["tables"]:
                        if t["tableW"] > t["wrapW"] + 1 and not t["scrollable"]:
                            fail("table-occluded", "%dx%d %s wrap=%s" % (w, h, path, t["wrapClass"]))
                    if m["small"]:
                        fail("small-target", "%dx%d %s %s" % (w, h, path, m["small"]))
                ctx.close()

            # 键盘可达 + 焦点可见 + 对比度 + 减少动效（在 1360 宽度下测一次）
            ctx = br.new_context(viewport={"width": 1360, "height": 860}, locale="zh-CN",
                                 reduced_motion="reduce")
            ident = json.dumps({"username": admin["username"], "role": admin["role"],
                                "permissions": admin["permissions"]})
            ctx.add_init_script("sessionStorage.setItem('er.admin.token', %s);"
                                "sessionStorage.setItem('er_admin_identity', %s);"
                                % (json.dumps(token), json.dumps(ident)))
            ctx.route("**/admin/v1/**", forward)
            page = ctx.new_page()
            page.goto("http://127.0.0.1:%d/dashboard" % SPORT, wait_until="networkidle")
            page.keyboard.press("Tab")
            focused = page.evaluate("""() => {
              const el = document.activeElement;
              if (!el) return null;
              const cs = getComputedStyle(el);
              return {tag: el.tagName, cls: el.className, outline: cs.outlineWidth + ' ' + cs.outlineStyle,
                      shadow: cs.boxShadow, visible: cs.visibility, display: cs.display};
            }""")
            if not focused or focused["visible"] == "hidden":
                fail("keyboard-focus", str(focused))
            elif focused["outline"].startswith("0px") and focused["shadow"] in ("none", ""):
                fail("focus-not-visible", str(focused))

            contrast = page.evaluate(r"""() => {
              function parse(c) { const m = c.match(/rgba?\(([^)]+)\)/); if (!m) return null;
                const p = m[1].split(',').map(x => parseFloat(x)); return {r:p[0], g:p[1], b:p[2], a: p[3] === undefined ? 1 : p[3]}; }
              function lum(c) { const f = v => { v /= 255; return v <= 0.03928 ? v/12.92 : Math.pow((v+0.055)/1.055, 2.4); };
                return 0.2126*f(c.r) + 0.7152*f(c.g) + 0.0722*f(c.b); }
              function ratio(a, b) { const l1 = lum(a), l2 = lum(b); const hi = Math.max(l1,l2), lo = Math.min(l1,l2);
                return (hi + 0.05) / (lo + 0.05); }
              const el = document.querySelector('.metric span') || document.querySelector('.muted') || document.body;
              const cs = getComputedStyle(el);
              const bg = getComputedStyle(document.body).backgroundColor;
              const fg = parse(cs.color), b = parse(bg);
              return fg && b ? {ratio: ratio(fg, b), fg: cs.color, bg} : null;
            }""")
            if contrast and contrast["ratio"] < 4.5:
                fail("contrast", str(contrast))

            motion = page.evaluate("""() => {
              const btn = document.querySelector('button');
              if (!btn) return null;
              const cs = getComputedStyle(btn);
              return {transition: cs.transitionDuration, animation: cs.animationDuration};
            }""")
            # 允许 0s 或 0.01ms 级别（<10ms）
            def ms(v):
                v = (v or "").split(",")[0].strip()
                if v.endswith("ms"): return float(v[:-2])
                if v.endswith("s"): return float(v[:-1]) * 1000
                return 0.0
            if motion and (ms(motion["transition"]) > 10 or ms(motion["animation"]) > 10):
                fail("reduced-motion", str(motion))

            ctx.close()
            br.close()
        srv.shutdown()

        result = {"generatedAt": time.strftime("%Y-%m-%dT%H:%M:%S%z"), "viewports": VIEWPORTS,
                  "pages": PAGES, "screenshots": shots, "findings": findings}
        with open(os.path.join(OUT, "audit.json"), "w", encoding="utf-8") as f:
            json.dump(result, f, ensure_ascii=False, indent=2)
            f.write("\n")

        if findings:
            print("AUDIT FAILED (%d findings)" % len(findings))
            return 1
        print("screenshots:", len(shots))
        print("AUDIT OK")
        return 0
    finally:
        st.terminate()
        try:
            st.wait(timeout=5)
        except subprocess.TimeoutExpired:
            st.kill()


if __name__ == "__main__":
    sys.exit(main())
