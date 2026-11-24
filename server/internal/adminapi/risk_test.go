package adminapi

import (
	"context"
	"testing"

	"github.com/easyremote/server/internal/contract"
	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/store"
)

// ADM-API-06c：黑名单校验/审计/权限，告警列表可读。
func TestBlacklistAdmin(t *testing.T) {
	s, mem, _ := setup(t)
	ctxb := context.Background()
	risk := Actor{Username: "riskops", Role: RoleRisk}

	// 非法值。
	for _, req := range []contract.BlacklistAddRequest{
		{Kind: "ip", Value: "not-an-ip"},
		{Kind: "ip", Value: "10.0.0.0/99"},
		{Kind: "phone", Value: "138"},
		{Kind: "account", Value: "  "},
	} {
		if _, err := s.AddBlacklist(ctxb, risk, req); !errcode.Is(err, errcode.AdminConfigInvalid) {
			t.Fatalf("want invalid for %+v, got %v", req, err)
		}
	}
	// 正常加入（带 TTL），并写审计。
	row, err := s.AddBlacklist(ctxb, risk, contract.BlacklistAddRequest{Kind: "account", Value: "acc-bad", Note: "刷连接", TtlHours: 24})
	if err != nil {
		t.Fatal(err)
	}
	if row.ID == "" || row.Kind != "account" || row.ExpiresAt.IsZero() {
		t.Fatalf("row: %+v", row)
	}
	var addAudit bool
	for _, a := range listAudit(mem) {
		if a.Action == "risk.blacklist_add" && a.Result == "ok" {
			addAudit = true
		}
	}
	if !addAudit {
		t.Fatal("缺 risk.blacklist_add 审计")
	}
	// 重复加入。
	if _, err := s.AddBlacklist(ctxb, risk, contract.BlacklistAddRequest{Kind: "account", Value: "acc-bad"}); !errcode.Is(err, errcode.AdminConfigInvalid) {
		t.Fatalf("want duplicate invalid, got %v", err)
	}
	// 移除：原因必填；ops 无 risk.write。
	if err := s.RemoveBlacklist(ctxb, risk, row.ID, " "); !errcode.Is(err, errcode.AdminReasonRequired) {
		t.Fatalf("want reason required, got %v", err)
	}
	if err := s.RemoveBlacklist(ctxb, Actor{Username: "ops1", Role: RoleOps}, row.ID, "误报"); !errcode.Is(err, errcode.AdminForbidden) {
		t.Fatalf("want forbidden for ops, got %v", err)
	}
	if err := s.RemoveBlacklist(ctxb, risk, row.ID, "误报"); err != nil {
		t.Fatal(err)
	}
	if _, err := mem.GetBlacklist(ctxb, row.ID); err == nil {
		t.Fatal("entry should be removed")
	}
	// 告警列表：readonly 可读。
	if err := mem.RecordAlert(ctxb, &store.RiskAlert{Kind: "blacklist_block", Subject: "account:acc-bad", Detail: "test"}); err != nil {
		t.Fatal(err)
	}
	alerts, err := s.ListAlerts(ctxb, Actor{Username: "ro", Role: RoleReadonly}, store.Page{Limit: 10})
	if err != nil || len(alerts) != 1 || alerts[0].Subject != "account:acc-bad" {
		t.Fatalf("alerts: %v %+v", err, alerts)
	}
}
