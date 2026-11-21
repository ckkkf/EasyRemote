package stack

import (
	"context"
	"os"
	"testing"

	"github.com/easyremote/server/internal/store"
)

func TestOpenStoreDevFallsBackToMemory(t *testing.T) {
	t.Setenv("ER_ENV", "development")
	t.Setenv("DATABASE_URL", "")
	st, err := OpenStore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.(*store.Memory); !ok {
		t.Fatalf("dev should fall back to memory, got %T", st)
	}
	_ = st.Close()
}

func TestOpenStoreProductionRequiresDatabaseURL(t *testing.T) {
	t.Setenv("ER_ENV", "production")
	t.Setenv("DATABASE_URL", "")
	if _, err := OpenStore(context.Background()); err == nil {
		t.Fatal("production without DATABASE_URL must fail")
	}
}

func TestOpenSessionsDevFallsBackToMemory(t *testing.T) {
	t.Setenv("ER_ENV", "development")
	t.Setenv("REDIS_URL", "")
	svc, err := OpenSessions(context.Background(), 0)
	if err != nil || svc == nil {
		t.Fatalf("dev fallback: %v", err)
	}
}

func TestOpenSessionsProductionRequiresRedis(t *testing.T) {
	t.Setenv("ER_ENV", "production")
	t.Setenv("REDIS_URL", "")
	if _, err := OpenSessions(context.Background(), 0); err == nil {
		t.Fatal("production without REDIS_URL must fail")
	}
}

func TestOpenStoreProductionWithDSNUsesPG(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	t.Setenv("ER_ENV", "production")
	t.Setenv("DATABASE_URL", dsn)
	st, err := OpenStore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, ok := st.(*store.PG); !ok {
		t.Fatalf("want *store.PG, got %T", st)
	}
}
