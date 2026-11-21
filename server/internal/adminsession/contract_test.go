package adminsession_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/easyremote/server/internal/adminsession"
)

// 契约：内存实现与 Redis 实现必须表现一致。
func runContract(t *testing.T, newStore func(t *testing.T) adminsession.Store) {
	t.Run("issue_verify_revoke", func(t *testing.T) {
		store := newStore(t)
		svc := adminsession.New(store, time.Hour)
		ctx := context.Background()

		tok, _, err := svc.Issue(ctx, "adm1", "root", "super")
		if err != nil {
			t.Fatal(err)
		}
		got, ok := svc.Verify(ctx, tok)
		if !ok || got.Role != "super" {
			t.Fatalf("verify failed: %v %+v", ok, got)
		}
		if err := svc.Revoke(ctx, tok); err != nil {
			t.Fatal(err)
		}
		if _, ok := svc.Verify(ctx, tok); ok {
			t.Fatal("revoked token still valid")
		}
	})

	t.Run("revoke_all", func(t *testing.T) {
		store := newStore(t)
		svc := adminsession.New(store, time.Hour)
		ctx := context.Background()
		a, _, _ := svc.Issue(ctx, "adm1", "root", "super")
		b, _, _ := svc.Issue(ctx, "adm2", "ops", "ops")
		if err := svc.RevokeAllForAdmin(ctx, "adm1"); err != nil {
			t.Fatal(err)
		}
		if _, ok := svc.Verify(ctx, a); ok {
			t.Fatal("adm1 session should be revoked")
		}
		if _, ok := svc.Verify(ctx, b); !ok {
			t.Fatal("adm2 session should survive")
		}
	})

	t.Run("missing_token", func(t *testing.T) {
		svc := adminsession.New(newStore(t), time.Hour)
		if _, ok := svc.Verify(context.Background(), "nope"); ok {
			t.Fatal("unknown token accepted")
		}
	})
}

func TestMemoryStoreContract(t *testing.T) {
	runContract(t, func(t *testing.T) adminsession.Store { return adminsession.NewMemoryStore() })
}

func TestRedisStoreContract(t *testing.T) {
	dsn := os.Getenv("REDIS_URL")
	if dsn == "" {
		t.Skip("REDIS_URL not set")
	}
	runContract(t, func(t *testing.T) adminsession.Store {
		ctx := context.Background()
		store, err := adminsession.NewRedisStoreFromURL(ctx, dsn, time.Hour)
		if err != nil {
			t.Fatalf("redis: %v", err)
		}
		t.Cleanup(func() {
			_ = store.FlushTestKeys(ctx)
			_ = store.Close()
		})
		return store
	})
}
