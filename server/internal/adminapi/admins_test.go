package adminapi

import (
	"context"
	"testing"

	"github.com/easyremote/server/internal/contract"
	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/store"
)

// ADM-API-06a：管理员列表/改角色/启停，含自保护与"最后一个超管"保护。
func TestAdminManagementAndGuards(t *testing.T) {
	s, mem, _ := setup(t)
	ctxb := context.Background()
	for _, u := range []*store.AdminUser{
		{Username: "root", PasswordHash: "x", TOTPSecret: "s", Role: RoleSuper, Status: "active"},
		{Username: "ops1", PasswordHash: "x", Role: RoleOps, Status: "active"},
	} {
		if err := mem.PutAdmin(ctxb, u); err != nil {
			t.Fatal(err)
		}
	}
	super := Actor{Username: "root", Role: RoleSuper}

	list, err := s.ListAdmins(ctxb, super, store.Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("list: %+v", list)
	}
	// MFA 状态：root 有 TOTP，ops1 没有。
	for _, a := range list {
		if a.Username == "root" && !a.MfaEnabled {
			t.Fatal("root 应显示 MFA 开启")
		}
		if a.Username == "ops1" && a.MfaEnabled {
			t.Fatal("ops1 不应显示 MFA 开启")
		}
	}
	// ops 无 admin.read。
	if _, err := s.ListAdmins(ctxb, Actor{Username: "ops1", Role: RoleOps}, store.Page{Limit: 10}); !errcode.Is(err, errcode.AdminForbidden) {
		t.Fatalf("want forbidden, got %v", err)
	}

	var ops contract.AdminRow
	for _, a := range list {
		if a.Username == "ops1" {
			ops = a
		}
	}
	if err := s.UpdateAdmin(ctxb, super, ops.ID, RoleCS, "", "转岗客服"); err != nil {
		t.Fatal(err)
	}
	got, _ := mem.GetAdmin(ctxb, ops.ID)
	if got.Role != RoleCS {
		t.Fatalf("role not updated: %+v", got)
	}
	// 审计 ok。
	var roleAudit bool
	for _, a := range listAudit(mem) {
		if a.Action == "admin.update" && a.Result == "ok" {
			roleAudit = true
		}
	}
	if !roleAudit {
		t.Fatal("缺 admin.update 审计")
	}

	// 唯一超管保护：此时只有 root 是 active 超管，禁用自己必须被拒。
	rootID := ""
	for _, a := range list {
		if a.Username == "root" {
			rootID = a.ID
		}
	}
	if err := s.UpdateAdmin(ctxb, super, rootID, "", "disabled", "试试"); err == nil {
		t.Fatal("唯一超管禁用自己没有被保护")
	} else if !errcode.Is(err, errcode.AdminForbidden) {
		t.Fatalf("want forbidden, got %v", err)
	}
	// 有第二个超管后，自我降级是允许的（不再一刀切禁止改自己）。
	if err := mem.UpdateAdminRole(ctxb, ops.ID, RoleSuper); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAdmin(ctxb, super, rootID, RoleOps, "", "交接给新超管"); err != nil {
		t.Fatalf("有备份超管时应允许自我降级: %v", err)
	}
	// 缺原因 / 非法角色。
	if err := s.UpdateAdmin(ctxb, super, ops.ID, RoleCS, "", "  "); !errcode.Is(err, errcode.AdminReasonRequired) {
		t.Fatalf("want reason required, got %v", err)
	}
	if err := s.UpdateAdmin(ctxb, super, ops.ID, "boss", "", "x"); !errcode.Is(err, errcode.AdminConfigInvalid) {
		t.Fatalf("want config invalid, got %v", err)
	}
}
