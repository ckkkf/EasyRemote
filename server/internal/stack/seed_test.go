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
}
