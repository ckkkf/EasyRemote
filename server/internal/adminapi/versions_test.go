package adminapi

import (
	"context"
	"testing"

	"github.com/easyremote/server/internal/contract"
	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/store"
)

// ADM-API-06b：版本发布校验（状态与覆盖率一致）、下线需原因、权限与审计。
func TestVersionPublishAndRetire(t *testing.T) {
	s, mem, _ := setup(t)
	ctxb := context.Background()
	ops := Actor{Username: "ops1", Role: RoleOps}

	// 非法平台 / draft 带覆盖率 / full 不足 100。
	if _, err := s.PublishVersion(ctxb, ops, contract.VersionPublishRequest{Platform: "linux", Version: "1.0", Status: "draft"}); !errcode.Is(err, errcode.AdminConfigInvalid) {
		t.Fatalf("want invalid platform, got %v", err)
	}
	if _, err := s.PublishVersion(ctxb, ops, contract.VersionPublishRequest{Platform: "windows", Version: "1.0", Status: "draft", RolloutPercent: 10}); !errcode.Is(err, errcode.AdminConfigInvalid) {
		t.Fatalf("want draft rollout invalid, got %v", err)
	}
	if _, err := s.PublishVersion(ctxb, ops, contract.VersionPublishRequest{Platform: "windows", Version: "1.0", Status: "full", RolloutPercent: 50}); !errcode.Is(err, errcode.AdminConfigInvalid) {
		t.Fatalf("want full rollout invalid, got %v", err)
	}
	// 正常灰度发布。
	row, err := s.PublishVersion(ctxb, ops, contract.VersionPublishRequest{
		Platform: "windows", Version: "0.2.13", Status: "rolling", RolloutPercent: 20, Notes: "灰度 20%",
	})
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "rolling" || row.RolloutPercent != 20 || row.ID == "" {
		t.Fatalf("row: %+v", row)
	}
	// 审计。
	var publishAudit bool
	for _, a := range listAudit(mem) {
		if a.Action == "version.publish" && a.Result == "ok" {
			publishAudit = true
		}
	}
	if !publishAudit {
		t.Fatal("缺 version.publish 审计")
	}
	// 下线：原因必填；只读角色被拒。
	if err := s.RetireVersion(ctxb, ops, row.ID, "  "); !errcode.Is(err, errcode.AdminReasonRequired) {
		t.Fatalf("want reason required, got %v", err)
	}
	if err := s.RetireVersion(ctxb, Actor{Username: "ro", Role: RoleReadonly}, row.ID, "旧版"); !errcode.Is(err, errcode.AdminForbidden) {
		t.Fatalf("want forbidden, got %v", err)
	}
	if err := s.RetireVersion(ctxb, ops, row.ID, "旧版下线"); err != nil {
		t.Fatal(err)
	}
	got, _ := mem.GetVersion(ctxb, row.ID)
	if got.Status != "retired" {
		t.Fatalf("status: %+v", got)
	}
	// 列表权限：readonly 可读。
	list, err := s.ListVersions(ctxb, Actor{Username: "ro", Role: RoleReadonly}, store.Page{Limit: 10})
	if err != nil || len(list) != 1 {
		t.Fatalf("readonly list: %v %+v", err, list)
	}
}
