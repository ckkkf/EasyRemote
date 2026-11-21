//go:build integration

// Package integration 跑完整业务链路并覆盖关键安全边界。
package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/easyremote/server/internal/adminapi"
	"github.com/easyremote/server/internal/stack"
)

type client struct {
	base string
	t    *testing.T
}

type reqOpts struct {
	bearer      string // 账号 access token
	deviceToken string // 设备凭证
}

func (c *client) do(method, path string, opts reqOpts, body any) (int, map[string]any) {
	c.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		raw, _ := json.Marshal(body)
		buf.Write(raw)
	}
	req, _ := http.NewRequest(method, c.base+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if opts.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+opts.bearer)
	}
	if opts.deviceToken != "" {
		req.Header.Set("X-Device-Token", opts.deviceToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	// 成功响应是信封 {code:0,name:OK,data:{...},trace_id}（AGENTS.md 铁律 1）。
	// 测试关心的是业务字段，所以成功时把 data 摊平出来，错误时保留信封字段。
	if _, hasCode := out["code"]; hasCode {
		if data, ok := out["data"].(map[string]any); ok && out["name"] == "OK" {
			return resp.StatusCode, data
		}
	}
	return resp.StatusCode, out
}

func setup(t *testing.T) *client {
	t.Helper()
	_, h, err := stack.NewDev()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &client{base: srv.URL, t: t}
}

type fixture struct {
	ctrlToken       string
	ctrlDevice      string
	ctrlDeviceToken string
	targetToken     string
	targetDevice    string
	targetCode      string
	deviceToken     string
}

func login(t *testing.T, c *client, phone string) string {
	t.Helper()
	code, body := c.do("POST", "/v1/auth/login", reqOpts{}, map[string]string{"phone": phone, "code": "123456"})
	if code != 200 {
		t.Fatalf("login: %d %v", code, body)
	}
	if _, ok := body["token"].(string); !ok {
		t.Logf("DBG_LOGIN phone=%s body=%v", phone, body)
	}
	return body["token"].(string)
}

func bind(t *testing.T, c *client, tok string) (deviceID, rawCode, deviceToken string) {
	t.Helper()
	code, body := c.do("POST", "/v1/devices/bind", reqOpts{bearer: tok}, map[string]string{"platform": "windows"})
	if code != 200 {
		t.Fatalf("bind: %d %v", code, body)
	}
	return body["device_id"].(string), body["raw_code"].(string), body["device_token"].(string)
}

func newFixture(t *testing.T, c *client) fixture {
	t.Helper()
	f := fixture{}
	f.ctrlToken = login(t, c, "13800008882")
	_, f.ctrlDevice, f.ctrlDeviceToken = bind(t, c, f.ctrlToken)
	f.targetToken = login(t, c, "13900001111")
	_, f.targetCode, f.deviceToken = bind(t, c, f.targetToken)
	f.targetDevice = ""
	return f
}

func TestFullFlow(t *testing.T) {
	c := setup(t)
	f := newFixture(t, c)

	// 被控端用设备凭证轮换验证码
	code, body := c.do("POST", "/v1/devices/verify-code",
		reqOpts{deviceToken: f.deviceToken}, map[string]string{"device_code": f.targetCode})
	if code != 200 {
		t.Fatalf("rotate: %d %v", code, body)
	}
	captcha := body["code"].(string)

	// 主控发起连接（账号 token + 验证码）
	code, body = c.do("POST", "/v1/connect/request", reqOpts{bearer: f.ctrlToken, deviceToken: f.ctrlDeviceToken},
		map[string]any{"device_code": f.targetCode, "captcha": captcha})
	if code != 200 {
		t.Fatalf("request: %d %v", code, body)
	}
	sessionID := body["session_id"].(string)

	// 被控端用自己的设备凭证授权
	code, body = c.do("POST", "/v1/connect/consent",
		reqOpts{deviceToken: f.deviceToken}, map[string]any{"session_id": sessionID, "accepted": true})
	if code != 200 {
		t.Fatalf("consent: %d %v", code, body)
	}
	if body["state"] != "authorized" {
		t.Fatalf("want authorized, got %v", body)
	}

	// 会话方领取中继票据
	code, body = c.do("POST", "/v1/relay/ticket",
		reqOpts{deviceToken: f.deviceToken}, map[string]any{"session_id": sessionID})
	if code != 200 {
		t.Fatalf("ticket: %d %v", code, body)
	}

	// 后台登录拿服务端会话令牌
	totp := adminapi.TOTPCode(stack.DevTOTPSecret, time.Now())
	code, body = c.do("POST", "/admin/v1/login", reqOpts{},
		map[string]string{"username": "root", "password": "dev-pass", "totp": totp})
	if code != 200 {
		t.Fatalf("admin login: %d %v", code, body)
	}
	adminTok := body["token"].(string)

	// 后台中断会话（必填原因）
	code, body = c.do("POST", "/admin/v1/sessions/kill", reqOpts{bearer: adminTok},
		map[string]any{"session_id": sessionID, "reason": "user report"})
	if code != 200 {
		t.Fatalf("kill: %d %v", code, body)
	}

	// 会话已关闭
	code, body = c.do("GET", "/v1/connect/status?session_id="+sessionID, reqOpts{deviceToken: f.deviceToken}, nil)
	if code != 200 || body["state"] != "closed" {
		t.Fatalf("status: %d %v", code, body)
	}

	// 审计里必须有 login / kill 记录
	code, body = c.do("GET", "/admin/v1/audit", reqOpts{bearer: adminTok}, nil)
	if code != 200 {
		t.Fatalf("audit: %d %v", code, body)
	}
	var hasKill, hasLogin bool
	for _, it := range body["items"].([]any) {
		m := it.(map[string]any)
		if m["action"] == "session.kill" && m["result"] == "ok" {
			hasKill = true
		}
		if m["action"] == "admin.login" {
			hasLogin = true
		}
	}
	if !hasKill || !hasLogin {
		t.Fatalf("audit incomplete: kill=%v login=%v", hasKill, hasLogin)
	}
}

// ---- 安全回归：这些用例锁死已修复的阻断项，防止回退 ----

func TestAdminForgedTokenRejected(t *testing.T) {
	c := setup(t)
	// 历史漏洞：base64("attacker:super") 曾被当作 super 身份
	forged := "YXR0YWNrZXI6c3VwZXI="
	code, _ := c.do("GET", "/admin/v1/users", reqOpts{bearer: forged}, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("forged admin token must be 401, got %d", code)
	}
	code, _ = c.do("GET", "/admin/v1/stats/overview", reqOpts{bearer: forged}, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("forged admin token on overview must be 401, got %d", code)
	}
}

func TestVerifyCodeRequiresDeviceCredential(t *testing.T) {
	c := setup(t)
	f := newFixture(t, c)
	// 无设备凭证：任何人不得轮换他人设备的验证码
	code, body := c.do("POST", "/v1/devices/verify-code", reqOpts{}, map[string]string{"device_code": f.targetCode})
	if code == 200 {
		t.Fatalf("verify-code without device credential must fail, got %v", body)
	}
	// 错误凭证同样拒绝
	code, _ = c.do("POST", "/v1/devices/verify-code",
		reqOpts{deviceToken: "wrong"}, map[string]string{"device_code": f.targetCode})
	if code == 200 {
		t.Fatal("verify-code with wrong credential must fail")
	}
}

func TestConsentRequiresTargetDeviceCredential(t *testing.T) {
	c := setup(t)
	f := newFixture(t, c)
	_, body := c.do("POST", "/v1/devices/verify-code", reqOpts{deviceToken: f.deviceToken},
		map[string]string{"device_code": f.targetCode})
	captcha := body["code"].(string)
	_, body = c.do("POST", "/v1/connect/request", reqOpts{bearer: f.ctrlToken, deviceToken: f.ctrlDeviceToken},
		map[string]any{"device_code": f.targetCode, "captcha": captcha})
	sessionID := body["session_id"].(string)

	// 攻击者替被控端点同意：必须失败
	code, _ := c.do("POST", "/v1/connect/consent", reqOpts{},
		map[string]any{"session_id": sessionID, "accepted": true})
	if code == 200 {
		t.Fatal("consent without device credential must fail")
	}
}

func TestRelayTicketRequiresPartyCredential(t *testing.T) {
	c := setup(t)
	f := newFixture(t, c)
	_, body := c.do("POST", "/v1/devices/verify-code", reqOpts{deviceToken: f.deviceToken},
		map[string]string{"device_code": f.targetCode})
	captcha := body["code"].(string)
	_, body = c.do("POST", "/v1/connect/request", reqOpts{bearer: f.ctrlToken, deviceToken: f.ctrlDeviceToken},
		map[string]any{"device_code": f.targetCode, "captcha": captcha})
	sessionID := body["session_id"].(string)
	c.do("POST", "/v1/connect/consent", reqOpts{deviceToken: f.deviceToken},
		map[string]any{"session_id": sessionID, "accepted": true})

	code, _ := c.do("POST", "/v1/relay/ticket", reqOpts{}, map[string]any{"session_id": sessionID})
	if code == 200 {
		t.Fatal("ticket without party credential must fail")
	}
}

func TestConsentRejectedPropagates(t *testing.T) {
	c := setup(t)
	f := newFixture(t, c)
	_, body := c.do("POST", "/v1/devices/verify-code", reqOpts{deviceToken: f.deviceToken},
		map[string]string{"device_code": f.targetCode})
	captcha := body["code"].(string)
	_, body = c.do("POST", "/v1/connect/request", reqOpts{bearer: f.ctrlToken, deviceToken: f.ctrlDeviceToken},
		map[string]any{"device_code": f.targetCode, "captcha": captcha})
	sessionID := body["session_id"].(string)

	code, body := c.do("POST", "/v1/connect/consent", reqOpts{deviceToken: f.deviceToken},
		map[string]any{"session_id": sessionID, "accepted": false, "reason": "stranger"})
	if code == 200 {
		t.Fatalf("rejected consent should not be 200: %v", body)
	}
	if name, _ := body["name"].(string); name != "CONN_CONSENT_REJECTED" {
		t.Fatalf("want CONN_CONSENT_REJECTED, got %v", body)
	}
}

func TestDeviceNotFoundMessageIsGeneric(t *testing.T) {
	c := setup(t)
	tok := login(t, c, "13800008882")
	bind(t, c, tok)
	code, body := c.do("POST", "/v1/connect/request", reqOpts{bearer: tok},
		map[string]any{"device_code": "000000000", "captcha": "1"})
	if code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", code)
	}
	if name, _ := body["name"].(string); name != "CONN_DEVICE_NOT_FOUND" {
		t.Fatalf("want CONN_DEVICE_NOT_FOUND, got %v", body)
	}
	// 用户可见文案必须是统一说法，不区分「设备不存在」与「未开启被控」（防枚举）
	msg, _ := body["message"].(string)
	if !strings.Contains(msg, "设备不存在或未开启被控") {
		t.Fatalf("message should be generic, got %q", msg)
	}
	// 信封必须带 trace_id 与 retryable
	if body["trace_id"] == nil || body["trace_id"] == "" {
		t.Fatalf("missing trace_id in error envelope: %v", body)
	}
	if _, ok := body["retryable"]; !ok {
		t.Fatalf("missing retryable in error envelope: %v", body)
	}
}

func TestSelfConnectRejected(t *testing.T) {
	c := setup(t)
	tok := login(t, c, "13800008882")
	_, myCode, devTok := bind(t, c, tok)
	_, body := c.do("POST", "/v1/devices/verify-code", reqOpts{deviceToken: devTok},
		map[string]string{"device_code": myCode})
	captcha := body["code"].(string)
	code, resp := c.do("POST", "/v1/connect/request", reqOpts{bearer: tok, deviceToken: devTok},
		map[string]any{"device_code": myCode, "captcha": captcha})
	if code != http.StatusBadRequest || resp["name"] != "CONN_SELF_CONNECT" {
		t.Fatalf("want CONN_SELF_CONNECT, got %d %v", code, resp)
	}
}

func TestUnauthenticatedDeviceListRejected(t *testing.T) {
	c := setup(t)
	code, _ := c.do("GET", "/v1/devices", reqOpts{}, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", code)
	}
}

// 回归：同账号绑多台设备时，主控设备必须由设备凭证确定，
// 不能取列表里第一台（那会导致 /v1/relay/ticket 用真实凭证校验时 700005）。
func TestMultiDeviceControllerIdentity(t *testing.T) {
	c := setup(t)
	ctrlTok := login(t, c, "13800007777")

	// 先绑一台"老"设备，再绑本次真正使用的新设备
	_, _, oldDevTok := bind(t, c, ctrlTok)
	newDevID, _, newDevTok := bind(t, c, ctrlTok)
	if oldDevTok == newDevTok {
		t.Fatal("two binds must yield different device tokens")
	}

	// 被控端
	targetTok := login(t, c, "13900007777")
	_, targetCode, targetDevTok := bind(t, c, targetTok)
	_, body := c.do("POST", "/v1/devices/verify-code", reqOpts{deviceToken: targetDevTok},
		map[string]string{"device_code": targetCode})
	captcha := body["code"].(string)

	// 用新设备的凭证发起连接
	code, sess := c.do("POST", "/v1/connect/request",
		reqOpts{bearer: ctrlTok, deviceToken: newDevTok},
		map[string]any{"device_code": targetCode, "captcha": captcha})
	if code != 200 {
		t.Fatalf("connect request: %d %v", code, sess)
	}
	sessionID := sess["session_id"].(string)

	// 被控端授权
	if code, b := c.do("POST", "/v1/connect/consent",
		reqOpts{deviceToken: targetDevTok},
		map[string]any{"session_id": sessionID, "accepted": true}); code != 200 {
		t.Fatalf("consent: %d %v", code, b)
	}

	// 关键断言：主控侧用新设备凭证能领到票据（旧实现这里必 700005）
	if code, b := c.do("POST", "/v1/relay/ticket",
		reqOpts{deviceToken: newDevTok},
		map[string]any{"session_id": sessionID}); code != 200 {
		t.Fatalf("relay ticket with the device actually used must succeed, got %d %v", code, b)
	}

	// 会话里的 controller 必须是新设备
	code, keys := c.do("GET", "/v1/connect/keys?session_id="+sessionID, reqOpts{bearer: ctrlTok}, nil)
	if code != 200 {
		t.Fatalf("read keys: %d %v", code, keys)
	}
	if keys["controller_id"] != newDevID {
		t.Fatalf("controller must be the requesting device %s, got %v", newDevID, keys["controller_id"])
	}
}
