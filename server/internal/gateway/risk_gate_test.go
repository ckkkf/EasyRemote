package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/store"
)

// ADM-API-06c：命中账号黑名单时连接必须被拒绝，并留下一条 blacklist_block 告警；
// 移除黑名单后同一请求必须恢复成功（证明闸门可逆、不是永久拒绝）。
func TestConnectBlockedByAccountBlacklist(t *testing.T) {
	f := newFixture(t)
	target := f.loginAs("13800009001")
	controller := f.loginAs("13800009002")
	f.prepareTarget(t, target)

	ctxb := context.Background()
	if err := f.mem.AddBlacklist(ctxb, &store.BlacklistEntry{
		Kind: "account", Value: controller.AccountID, Note: "风控测试", CreatedBy: "risk",
	}); err != nil {
		t.Fatalf("blacklist: %v", err)
	}

	status, raw := f.requestConnect(t, target, controller)
	if status == http.StatusOK {
		t.Fatalf("blocked connect should fail, got 200: %s", raw)
	}
	var env struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode: %v (%s)", err, raw)
	}
	if env.Code != int(errcode.ConnRateLimited) {
		t.Fatalf("code=%d want %d (%s)", env.Code, errcode.ConnRateLimited, raw)
	}

	alerts, err := f.mem.ListAlerts(ctxb, store.Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 || alerts[0].Kind != "blacklist_block" || alerts[0].Subject != "account:"+controller.AccountID {
		t.Fatalf("alerts: %+v", alerts)
	}

	// 移除黑名单后恢复。
	entries, err := f.mem.ListBlacklist(ctxb, store.Page{Limit: 10})
	if err != nil || len(entries) != 1 {
		t.Fatalf("list blacklist: %v %+v", err, entries)
	}
	if err := f.mem.RemoveBlacklist(ctxb, entries[0].ID); err != nil {
		t.Fatal(err)
	}
	if status, raw := f.requestConnect(t, target, controller); status != http.StatusOK {
		t.Fatalf("after removal status=%d body=%s", status, raw)
	}
}
