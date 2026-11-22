package relay_test

import (
	"context"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/easyremote/server/internal/relay"
)

func startRedis(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatalf("需要 docker 启动 Redis: %v", err)
	}
	out, err := exec.Command("docker", "run", "--rm", "-d", "-p", "127.0.0.1::6379", "redis:7-alpine").Output()
	if err != nil {
		t.Fatalf("docker run redis: %v", err)
	}
	id := strings.TrimSpace(string(out))
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", id).Run() })
	portOut, err := exec.Command("docker", "port", id, "6379/tcp").Output()
	if err != nil {
		t.Fatalf("docker port: %v", err)
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

// 关闭后旧票据即使 HMAC 仍有效，也必须被撤销键拒绝。
func TestRevokedTicketRejectedAfterClose(t *testing.T) {
	url := startRedis(t)
	ctx := context.Background()
	reg, err := relay.NewRedisRegistryFromURL(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()

	if relay.IsSessionRevoked(ctx, reg, "ses-old") {
		t.Fatal("fresh session must not be revoked")
	}
	if err := reg.Revoke(ctx, "ses-old", time.Hour); err != nil {
		t.Fatal(err)
	}
	if !relay.IsSessionRevoked(ctx, reg, "ses-old") {
		t.Fatal("revoked session must be rejected")
	}
}
