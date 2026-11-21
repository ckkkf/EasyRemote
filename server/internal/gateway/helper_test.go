package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/easyremote/server/internal/appconfig"
	"github.com/easyremote/server/internal/auth"
	"github.com/easyremote/server/internal/device"
	"github.com/easyremote/server/internal/ratelimit"
	"github.com/easyremote/server/internal/relaymgr"
	"github.com/easyremote/server/internal/session"
	"github.com/easyremote/server/internal/store"
)

// fixture 是一套「真路由 + 内存存储」的网关测试环境。
//
// 走真路由而不是直接调 handler：路由注册、中间件、错误信封都属于被测行为，
// 直接调 handler 会漏掉这些。
type fixture struct {
	t      *testing.T
	app    *App
	srv    *httptest.Server
	mem    *store.Memory
	cfg    *appconfig.Service
	acct   *accountFixture
	client *http.Client
}

type accountFixture struct {
	Token       string `json:"token"`
	AccountID   string `json:"account_id"`
	DeviceID    string `json:"device_id"`
	DeviceCode  string `json:"raw_code"`
	DeviceToken string `json:"device_token"`
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	mem := store.NewMemory()
	secret := []byte("gateway-test-secret")
	authSvc, err := auth.New(mem, nil, secret)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	app := &App{
		Limiter:  ratelimit.NewMemory(),
		Auth:     authSvc,
		Device:   device.New(mem, secret),
		Session:  session.New(mem),
		Relay:    relaymgr.New(secret, time.Hour),
		Store:    mem,
		Config:   appconfig.New(mem),
		FilesDir: t.TempDir(),
	}
	srv := httptest.NewServer(app.Routes())
	t.Cleanup(srv.Close)
	f := &fixture{t: t, app: app, srv: srv, mem: mem, cfg: app.Config, client: srv.Client()}
	f.acct = f.loginAs("13800001111")
	return f
}

// loginAs 走真实登录 + 绑定设备，返回该账号的凭证组合。
func (f *fixture) loginAs(phone string) *accountFixture {
	f.t.Helper()
	var out accountFixture
	f.postJSON("/v1/auth/login", map[string]any{"phone": phone, "code": "123456"}, nil, &out)
	body := map[string]any{}
	f.postJSON("/v1/devices/bind", map[string]any{"platform": "test"},
		map[string]string{"Authorization": "Bearer " + out.Token}, &body)
	out.AccountID = str(body["account_id"])
	out.DeviceID = str(body["device_id"])
	out.DeviceCode = str(body["raw_code"])
	out.DeviceToken = str(body["device_token"])
	if out.Token == "" || out.DeviceToken == "" {
		f.t.Fatalf("login/bind failed: %v", body)
	}
	return &out
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// do 发请求并返回状态码与响应体。
func (f *fixture) do(method, path string, body io.Reader, headers map[string]string) (int, []byte) {
	f.t.Helper()
	req, err := http.NewRequest(method, f.srv.URL+path, body)
	if err != nil {
		f.t.Fatalf("request: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := f.client.Do(req)
	if err != nil {
		f.t.Fatalf("do %s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, raw
}

func (f *fixture) postJSON(path string, payload any, headers map[string]string, out any) int {
	f.t.Helper()
	buf, err := json.Marshal(payload)
	if err != nil {
		f.t.Fatalf("marshal: %v", err)
	}
	h := map[string]string{"Content-Type": "application/json"}
	for k, v := range headers {
		h[k] = v
	}
	status, raw := f.do(http.MethodPost, path, bytes.NewReader(buf), h)
	if out != nil {
		if err := unmarshalData(raw, out); err != nil {
			f.t.Fatalf("decode %s: %v (body=%s)", path, err, raw)
		}
	}
	return status
}

// upload 构造一次 multipart 上传。
func (f *fixture) upload(acct *accountFixture, name string, content []byte) (int, []byte) {
	f.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", name)
	if err != nil {
		f.t.Fatalf("multipart: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		f.t.Fatalf("write part: %v", err)
	}
	_ = w.Close()
	return f.do(http.MethodPost, "/v1/files", &buf, map[string]string{
		"Content-Type":   w.FormDataContentType(),
		"Authorization":  "Bearer " + acct.Token,
		"X-Device-Token": acct.DeviceToken,
	})
}

// errName 从错误信封里取 name 字段（判断错误码用名字而不是数字）。
func errName(raw []byte) string {
	var env struct {
		Name string `json:"name"`
	}
	_ = unmarshalData(raw, &env)
	return env.Name
}

func containsAudit(t *testing.T, mem *store.Memory, action, result string) bool {
	t.Helper()
	logs, err := mem.ListAudit(t.Context(), 200)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	for _, l := range logs {
		if l.Action == action && l.Result == result {
			return true
		}
	}
	return false
}

func hasPrefix(s, p string) bool { return strings.HasPrefix(s, p) }

// unmarshalData 解成功信封里的 data；错误体（无 data）回退为裸体解析。
// 测试断言只关心业务体，信封形状由契约测试单独守。
func unmarshalData(raw []byte, out any) error {
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err == nil && len(env.Data) > 0 && string(env.Data) != "null" {
		return json.Unmarshal(env.Data, out)
	}
	return json.Unmarshal(raw, out)
}
