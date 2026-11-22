package stack

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/easyremote/server/internal/signal"
	"github.com/easyremote/server/internal/store"
)

func startRedisContainer(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("docker", "run", "--rm", "-d", "-p", "127.0.0.1::6379", "redis:7-alpine").Output()
	if err != nil {
		t.Fatalf("docker run redis: %v", err)
	}
	id := strings.TrimSpace(string(out))
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", id).Run() })
	portOut, err := exec.Command("docker", "port", id, "6379/tcp").Output()
	if err != nil {
		t.Fatalf("docker port redis: %v", err)
	}
	addr := strings.TrimSpace(strings.Split(string(portOut), "\n")[0])
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return "redis://" + addr
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("redis 未就绪")
	return ""
}

// ER-411：生产 PG+Redis 组合启动（多实例形态）且不 seed 开发管理员。
func TestProductionPostgresRedisStarts(t *testing.T) {
	dsn := startPostgres(t)
	redisURL := startRedisContainer(t)
	t.Setenv("ER_ENV", "production")
	t.Setenv("ER_SECRET", "0123456789abcdef0123456789abcdef")
	ctx := context.Background()

	bus, err := signal.NewRedisBus(ctx, redisURL, "smoke")
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()
	var st store.Store
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		st, _, err = NewPostgresWithRedis(ctx, dsn, redisURL,
			signal.ConsentNotifier{Bus: bus},
			signal.Presence{Bus: bus},
			signal.ConsentNotifier{Bus: bus})
		if err == nil {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("production pg+redis start: %v", err)
	}
	if _, err := st.GetAdminByUsername(ctx, "root"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("production must not seed root, err=%v", err)
	}
}
