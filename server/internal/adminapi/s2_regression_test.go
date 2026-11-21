package adminapi

import (
	"context"
	"testing"
	"time"

	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/session"
	"github.com/easyremote/server/internal/store"
)

// S2 回归：同一时间步内两个不同管理员都能登录（键按 admin 区分）。
func TestTwoAdminsSameStepBothLogin(t *testing.T) {
	mem := store.NewMemory()
	ctx := context.Background()
	_ = mem.PutAdmin(ctx, &store.AdminUser{
		Username: "a", PasswordHash: HashPassword("pw"), TOTPSecret: stdSecret, Role: RoleSuper, Status: "active",
	})
	_ = mem.PutAdmin(ctx, &store.AdminUser{
		Username: "b", PasswordHash: HashPassword("pw"), TOTPSecret: stdSecret, Role: RoleOps, Status: "active",
	})
	svc := New(mem, session.New(mem))
	at := time.Unix(1_700_000_000, 0).UTC()
	svc.SetClock(func() time.Time { return at })
	code := TOTPCode(stdSecret, at)

	if _, err := svc.Authenticate(ctx, "a", "pw", code); err != nil {
		t.Fatalf("admin a should pass: %v", err)
	}
	if _, err := svc.Authenticate(ctx, "b", "pw", code); err != nil {
		t.Fatalf("admin b should also pass in same step: %v", err)
	}
	// 同一管理员重复用同一码 → 重放
	if _, err := svc.Authenticate(ctx, "a", "pw", code); !errcode.Is(err, errcode.AdminMfaRequired) {
		t.Fatalf("replay for admin a must be rejected, got %v", err)
	}
}

// 后端故障必须报服务不可用，而不是伪装成重放
type failingGuard struct{}

func (failingGuard) Use(string, int64) (bool, error) {
	return false, context.DeadlineExceeded
}

func TestReplayBackendFailureIsServiceError(t *testing.T) {
	mem := store.NewMemory()
	ctx := context.Background()
	_ = mem.PutAdmin(ctx, &store.AdminUser{
		Username: "root", PasswordHash: HashPassword("pw"), TOTPSecret: stdSecret, Role: RoleSuper, Status: "active",
	})
	svc := NewWithReplay(mem, session.New(mem), failingGuard{})
	at := time.Unix(1_700_000_000, 0).UTC()
	svc.SetClock(func() time.Time { return at })

	_, err := svc.Authenticate(ctx, "root", "pw", TOTPCode(stdSecret, at))
	if !errcode.Is(err, errcode.SrvMaintenance) {
		t.Fatalf("backend failure must map to SRV_MAINTENANCE, got %v", err)
	}
}
