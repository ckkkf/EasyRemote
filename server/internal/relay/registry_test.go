package relay

import (
	"context"
	"os"
	"testing"
	"time"
)

func runRegistryContract(t *testing.T, newReg func(t *testing.T) Registry) {
	ctx := context.Background()

	t.Run("node_directory", func(t *testing.T) {
		r := newReg(t)
		if err := r.RegisterNode(ctx, "node-a", "127.0.0.1:9001", time.Minute); err != nil {
			t.Fatal(err)
		}
		addr, err := r.LookupNode(ctx, "node-a")
		if err != nil || addr != "127.0.0.1:9001" {
			t.Fatalf("lookup node: %q %v", addr, err)
		}
	})

	t.Run("route_directory", func(t *testing.T) {
		r := newReg(t)
		if err := r.SetRoute(ctx, "ses-1", "devA", "node-a", time.Minute); err != nil {
			t.Fatal(err)
		}
		node, err := r.LookupRoute(ctx, "ses-1", "devA")
		if err != nil || node != "node-a" {
			t.Fatalf("lookup route: %q %v", node, err)
		}
		if err := r.ClearRoute(ctx, "ses-1", "devA"); err != nil {
			t.Fatal(err)
		}
		if _, err := r.LookupRoute(ctx, "ses-1", "devA"); err == nil {
			t.Fatal("route should be cleared")
		}
	})

	t.Run("missing_returns_error", func(t *testing.T) {
		r := newReg(t)
		if _, err := r.LookupNode(ctx, "nope"); err == nil {
			t.Fatal("missing node must error")
		}
		if _, err := r.LookupRoute(ctx, "nope", "nope"); err == nil {
			t.Fatal("missing route must error")
		}
	})
}

func TestMemoryRegistryContract(t *testing.T) {
	runRegistryContract(t, func(t *testing.T) Registry { return NewMemoryRegistry() })
}

func TestRedisRegistryContract(t *testing.T) {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		t.Skip("REDIS_URL not set")
	}
	runRegistryContract(t, func(t *testing.T) Registry {
		r, err := NewRedisRegistryFromURL(context.Background(), url)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			ctx := context.Background()
			_ = r.ClearRoute(ctx, "ses-1", "devA")
			_ = r.Close()
		})
		return r
	})
}
