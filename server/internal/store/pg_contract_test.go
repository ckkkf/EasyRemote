//go:build integration

package store_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/easyremote/server/internal/store"
	"github.com/easyremote/server/internal/store/storetest"
)

// 与内存实现跑完全相同的契约用例——这是「PG 实现不能靠肉眼保证正确」的落地。
func TestPGContract(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping PG contract test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pg, err := store.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = pg.Close() })

	ddl, err := os.ReadFile("../../migrations/001_init.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	// 先建表（幂等），再清数据，保证契约可重复执行
	if err := pg.Migrate(ctx, string(ddl)); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := pg.ExecRaw(ctx, `
		TRUNCATE TABLE audit_logs, sessions, trust_edges, verify_codes, files, assist_codes, app_config, devices, accounts, admin_users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	storetest.Run(t, func(t *testing.T) store.Store {
		// 每个子测试前清空，保证用例之间互不干扰
		if _, err := pg.ExecRaw(context.Background(),
			`TRUNCATE TABLE audit_logs, sessions, trust_edges, verify_codes, files, assist_codes, app_config, devices, accounts, admin_users RESTART IDENTITY CASCADE`); err != nil {
			t.Fatalf("truncate: %v", err)
		}
		return pg
	})
}

func TestPGPing(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	pg, err := store.NewPG(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	if err := pg.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
}
