package adminhttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/easyremote/server/internal/adminapi"
	"github.com/easyremote/server/internal/adminhttp"
	"github.com/easyremote/server/internal/adminsession"
	"github.com/easyremote/server/internal/contract"
	"github.com/easyremote/server/internal/session"
	"github.com/easyremote/server/internal/store"
)

type violation struct {
	id     string
	path   string
	reason string
}

// TestAdminEnvelopeContract 校验后台每条路由的响应形状。
//
// 默认报告模式：打印违规但不失败，用于存量迁移；
// ER_ENVELOPE_STRICT=1 时违规即失败（scripts/gate.sh --strict）。
// 接口清单来自 server/internal/contract/types_gen.go，即 docs/spec/contract.yaml。
func TestAdminEnvelopeContract(t *testing.T) {
	st := store.NewMemory()
	sess := adminsession.New(nil, time.Hour)
	tok, _, err := sess.Issue(context.Background(), "admin-contract", "root", adminapi.RoleSuper)
	if err != nil {
		t.Fatalf("签发测试会话失败: %v", err)
	}
	app := &adminhttp.App{Service: adminapi.New(st, session.New(st)), Sessions: sess}
	h := app.Routes()

	var bad []violation
	add := func(id, path, reason string) { bad = append(bad, violation{id: id, path: path, reason: reason}) }

	check := func(id, path string, rec *httptest.ResponseRecorder, wantOK bool) {
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			add(id, path, "content-type="+ct)
			return
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			add(id, path, "响应不是合法 JSON: "+err.Error())
			return
		}
		for _, k := range []string{"code", "name", "trace_id"} {
			if _, ok := body[k]; !ok {
				add(id, path, "缺字段 "+k)
				return
			}
		}
		if !wantOK {
			return
		}
		if code, _ := body["code"].(float64); code != 0 {
			add(id, path, "成功响应 code 应为 0")
			return
		}
		if _, ok := body["data"]; !ok {
			add(id, path, "成功响应缺 data")
		}
	}

	do := func(id, method, path, token string, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			// 每条请求用独立会话：logout 会撤销令牌，复用同一个 tok 会让
			// 后续所有接口变成 ADMIN_SESSION_EXPIRED（历史上就是这样被掩盖的）。
			fresh, _, err := sess.Issue(context.Background(), "admin-contract", "root", adminapi.RoleSuper)
			if err != nil {
				t.Fatalf("签发测试会话失败: %v", err)
			}
			req.Header.Set("Authorization", "Bearer "+fresh)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	for _, ep := range contract.Endpoints {
		if !strings.HasPrefix(ep.Path, "/admin/") {
			continue
		}
		body := "{}"
		wantOK := ep.Method == http.MethodGet && ep.Auth == "admin"
		check(ep.ID, ep.Path, do(ep.ID, ep.Method, ep.Path, tok, body), wantOK)
		// 未鉴权路径必须返回错误信封，而不是裸 401 文本。
		check(ep.ID+"-noauth", ep.Path, do(ep.ID, ep.Method, ep.Path, "", body), false)
	}

	// 兜底：未匹配路由必须是 JSON 信封（700011），不能是 text/plain。
	check("FALLBACK-404", "/admin/v1/does-not-exist", do("FALLBACK-404", http.MethodGet, "/admin/v1/does-not-exist", tok, ""), false)

	// 健康检查也必须走信封。
	check("HEALTH", "/healthz", do("HEALTH", http.MethodGet, "/healthz", "", ""), true)

	if len(bad) == 0 {
		t.Logf("全部 %d 条接口响应形状已对齐", len(contract.Endpoints))
		return
	}
	for _, v := range bad {
		t.Logf("违规 %-16s %-32s %s", v.id, v.path, v.reason)
	}
	if os.Getenv("ER_ENVELOPE_STRICT") == "1" {
		t.Fatalf("信封未对齐 %d 处（ER_ENVELOPE_STRICT=1）", len(bad))
	}
	t.Logf("报告模式：%d 处待迁移；全部迁移后改用 bash scripts/gate.sh --strict 收口", len(bad))
}
