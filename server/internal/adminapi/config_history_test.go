package adminapi

import (
	"context"
	"strings"
	"testing"

	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/store"
)

// ADM-API-05b：配置变更历史来自审计；回滚走同一套校验并写 config.rollback 审计。
func TestConfigHistoryAndRollback(t *testing.T) {
	s, mem, _ := setup(t)
	ctxb := context.Background()
	super := Actor{Username: "root", Role: RoleSuper}

	if _, err := s.PutConfig(ctxb, super, map[string]string{"session.max_minutes": "600"}); err != nil {
		t.Fatal(err)
	}
	hist, err := s.ConfigHistory(ctxb, super, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist.Items) == 0 {
		t.Fatal("history 应有至少一条 config.update")
	}
	first := hist.Items[0]
	if first.Action != "config.update" || first.Actor != "root" || !strings.Contains(first.After, "600") {
		t.Fatalf("history entry: %+v", first)
	}
	// config.read 只有 super；ops 必须被拒。
	if _, err := s.ConfigHistory(ctxb, Actor{Username: "ops", Role: RoleOps}, 10); !errcode.Is(err, errcode.AdminForbidden) {
		t.Fatalf("want forbidden, got %v", err)
	}

	cfg, err := s.RollbackConfig(ctxb, super, "session.max_minutes", "720")
	if err != nil {
		t.Fatal(err)
	}
	if cfg["session.max_minutes"] != "720" {
		t.Fatalf("rollback value: %q", cfg["session.max_minutes"])
	}
	audits := listAudit(mem)
	var rollback *store.AuditLog
	for _, a := range audits {
		if a.Action == "config.rollback" {
			rollback = a
		}
	}
	if rollback == nil || rollback.Object != "session.max_minutes" {
		t.Fatalf("rollback audit: %+v", audits)
	}
	if !strings.Contains(rollback.After, "720") {
		t.Fatalf("rollback audit after: %q", rollback.After)
	}

	// 未知 key 必须被拒并留痕。
	if _, err := s.RollbackConfig(ctxb, super, "nope.key", "1"); !errcode.Is(err, errcode.AdminConfigInvalid) {
		t.Fatalf("want config invalid, got %v", err)
	}
}
