package adminapi

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/easyremote/server/internal/store"
)

// ADM-API-03e：用户列表按注册时间过滤 + CSV 导出（脱敏 + 审计）。
func TestUsersTimeRangeAndExport(t *testing.T) {
	s, mem, _ := setup(t)
	ctxb := context.Background()
	act := Actor{Username: "root", Role: RoleSuper}
	base := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)

	accounts := []*store.Account{
		{ID: "acc-old", PhoneEnc: "13800000001", PhoneHash: "hash-old", Status: "active", CreatedAt: base},
		{ID: "acc-new", PhoneEnc: "13800000002", PhoneHash: "hash-new", Status: "active", CreatedAt: base.Add(48 * time.Hour)},
	}
	for _, a := range accounts {
		if err := mem.CreateAccount(ctxb, a); err != nil {
			t.Fatal(err)
		}
	}

	// 时间范围：只取后半段。
	list, err := s.ListUsers(ctxb, act, store.Page{Limit: 10, From: base.Add(24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	// setup() 自己也 seed 了账号，所以只断言"窗口外的被排除、窗口内的还在"。
	found := map[string]bool{}
	for _, u := range list {
		found[u.ID] = true
	}
	if found["acc-old"] || !found["acc-new"] {
		t.Fatalf("time filter: %+v", found)
	}
	// 导出：表头 + 脱敏手机号 + 审计。
	res, err := s.ExportUsers(ctxb, act, store.Page{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if res.Rows < 2 || res.Truncated {
		t.Fatalf("rows: %+v", res)
	}
	if !strings.HasPrefix(res.Csv, "账号 ID,昵称,手机号") {
		t.Fatalf("header: %q", res.Csv)
	}
	if strings.Contains(res.Csv, "13800000001") {
		t.Fatal("CSV 泄露完整手机号")
	}
	if !strings.Contains(res.Csv, "138****0001") {
		t.Fatalf("CSV 缺脱敏手机号: %q", res.Csv)
	}
	var exportAudit bool
	for _, a := range listAudit(mem) {
		if a.Action == "user.export" && a.Result == "ok" {
			exportAudit = true
		}
	}
	if !exportAudit {
		t.Fatal("缺 user.export 审计")
	}
	// 截断。
	trunc, err := s.ExportUsers(ctxb, act, store.Page{}, 1)
	if err != nil || !trunc.Truncated || trunc.Rows != 1 {
		t.Fatalf("truncated: %v %+v", err, trunc)
	}
}
