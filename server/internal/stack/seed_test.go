package stack

import (
	"context"
	"errors"
	"testing"

	"github.com/easyremote/server/internal/store"
)

// ER-410：生产启动绝不能创建已知管理员；开发环境才 seed。
func TestProductionNeverSeedsDevAdmin(t *testing.T) {
	t.Setenv("ER_ENV", "production")
	mem := store.NewMemory()
	if err := seedDevAdminIfNotProduction(context.Background(), mem); err != nil {
		t.Fatal(err)
	}
	if _, err := mem.GetAdminByUsername(context.Background(), "root"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("production must not seed root admin, err=%v", err)
	}
	if _, err := mem.GetAdminByUsername(context.Background(), "ops-dev"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("production must not seed ops-dev, err=%v", err)
	}
	if _, err := mem.GetAdminByUsername(context.Background(), "ops2-dev"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("production must not seed ops2-dev, err=%v", err)
	}
}

func TestDevSeedsDevAdmin(t *testing.T) {
	t.Setenv("ER_ENV", "development")
	mem := store.NewMemory()
	if err := seedDevAdminIfNotProduction(context.Background(), mem); err != nil {
		t.Fatal(err)
	}
	if _, err := mem.GetAdminByUsername(context.Background(), "root"); err != nil {
		t.Fatalf("dev should seed root admin: %v", err)
	}
	// A14 页面需要一个可改角色的第二管理员；它必须是 disabled 且无 TOTP（不可登录）。
	ops, err := mem.GetAdminByUsername(context.Background(), "ops-dev")
	if err != nil {
		t.Fatalf("dev should seed ops-dev: %v", err)
	}
	if ops.Status != "disabled" || ops.TOTPSecret != "" {
		t.Fatalf("ops-dev must be disabled without totp: %+v", ops)
	}
	// ops2-dev 是可登录的第二操作人（双人确认 E2E 用），必须 active 且有 TOTP。
	ops2, err := mem.GetAdminByUsername(context.Background(), "ops2-dev")
	if err != nil {
		t.Fatalf("dev should seed ops2-dev: %v", err)
	}
	if ops2.Status != "active" || ops2.TOTPSecret == "" {
		t.Fatalf("ops2-dev must be active with totp: %+v", ops2)
	}
}
