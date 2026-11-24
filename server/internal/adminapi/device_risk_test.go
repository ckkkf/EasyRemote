package adminapi

import (
	"context"
	"testing"

	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/store"
)

// ADM-API-04c：标记/解除风险都要求原因并写审计；readonly 被拒。
func TestDeviceRiskFlagging(t *testing.T) {
	s, mem, _ := setup(t)
	ctxb := context.Background()
	d := &store.Device{ID: "dev-risk", DeviceCode: "970000001", AccountID: "acc1", Platform: "macOS"}
	if err := mem.CreateDevice(ctxb, d); err != nil {
		t.Fatal(err)
	}
	risk := Actor{Username: "riskops", Role: RoleRisk}
	if err := s.FlagDeviceRisk(ctxb, risk, d.ID, "同 IP 高频连接"); err != nil {
		t.Fatal(err)
	}
	got, _ := mem.GetDevice(ctxb, d.ID)
	if !got.RiskFlag || got.RiskReason != "同 IP 高频连接" {
		t.Fatalf("flag: %+v", got)
	}
	if err := s.ClearDeviceRisk(ctxb, risk, d.ID, "误报"); err != nil {
		t.Fatal(err)
	}
	got, _ = mem.GetDevice(ctxb, d.ID)
	if got.RiskFlag {
		t.Fatalf("clear failed: %+v", got)
	}
	// 缺原因 / 只读角色 / 未知设备。
	if err := s.FlagDeviceRisk(ctxb, risk, d.ID, "  "); !errcode.Is(err, errcode.AdminReasonRequired) {
		t.Fatalf("want reason required, got %v", err)
	}
	if err := s.FlagDeviceRisk(ctxb, Actor{Username: "ro", Role: RoleReadonly}, d.ID, "x"); !errcode.Is(err, errcode.AdminForbidden) {
		t.Fatalf("want forbidden, got %v", err)
	}
	if err := s.FlagDeviceRisk(ctxb, risk, "no-such", "x"); !errcode.Is(err, errcode.ConnDeviceNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
	// 审计里必须能找到 flag_risk 与 clear_risk。
	actions := map[string]int{}
	for _, a := range listAudit(mem) {
		actions[a.Action]++
	}
	if actions["device.flag_risk"] == 0 || actions["device.clear_risk"] == 0 {
		t.Fatalf("audit actions: %+v", actions)
	}
}
