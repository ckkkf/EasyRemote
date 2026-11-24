package adminhttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/easyremote/server/internal/adminapi"
	"github.com/easyremote/server/internal/adminhttp"
	"github.com/easyremote/server/internal/adminsession"
	"github.com/easyremote/server/internal/session"
	"github.com/easyremote/server/internal/store"
)

// TestListUsers_HTTPEnvelope：用户列表的成功响应必须是 {code,name,data,trace_id}，
// data.items 是数组、phone_mask 已脱敏（ADM-02-users 的卡内断言，ADM-02a 补齐）。
func TestListUsers_HTTPEnvelope(t *testing.T) {
	st := store.NewMemory()
	if err := st.CreateAccount(context.Background(), &store.Account{
		ID: "acc1", PhoneHash: "h1", PhoneEnc: "13800008882", Status: "active",
	}); err != nil {
		t.Fatal(err)
	}
	sess := adminsession.New(nil, time.Hour)
	tok, _, err := sess.Issue(context.Background(), "admin-users", "root", adminapi.RoleSuper)
	if err != nil {
		t.Fatal(err)
	}
	app := &adminhttp.App{Service: adminapi.New(st, session.New(st)), Sessions: sess}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/v1/users", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	app.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var body struct {
		Code    int    `json:"code"`
		Name    string `json:"name"`
		TraceID string `json:"trace_id"`
		Data    struct {
			Items []struct {
				ID        string `json:"id"`
				PhoneMask string `json:"phone_mask"`
				Status    string `json:"status"`
			} `json:"items"`
			Total int `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if body.Code != 0 || body.Name != "OK" {
		t.Fatalf("want code=0 name=OK, got %d/%s", body.Code, body.Name)
	}
	if body.TraceID == "" {
		t.Fatal("trace_id 不能为空")
	}
	if body.Data.Items == nil {
		t.Fatal("data.items 必须是数组，不能是 null")
	}
	if len(body.Data.Items) != 1 || body.Data.Total != 1 {
		t.Fatalf("want 1 item/total, got %d/%d", len(body.Data.Items), body.Data.Total)
	}
	maskRe := regexp.MustCompile("^[0-9]{3}\\*{4}[0-9]{4}$")
	if got := body.Data.Items[0].PhoneMask; !maskRe.MatchString(got) {
		t.Fatalf("phone_mask %q 未脱敏", got)
	}
}
