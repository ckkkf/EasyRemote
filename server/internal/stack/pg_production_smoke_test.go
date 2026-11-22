package stack

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/easyremote/server/internal/store"
)

func startPostgres(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatalf("需要 docker 启动 Postgres: %v", err)
	}
	out, err := exec.Command("docker", "run", "--rm", "-d",
		"-e", "POSTGRES_PASSWORD=smoke-pass",
		"-p", "127.0.0.1::5432", "postgres:17-alpine").Output()
	if err != nil {
		t.Fatalf("docker run postgres: %v", err)
	}
	id := strings.TrimSpace(string(out))
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", id).Run() })
	portOut, err := exec.Command("docker", "port", id, "5432/tcp").Output()
	if err != nil {
		t.Fatalf("docker port: %v", err)
	}
	addr := strings.TrimSpace(strings.Split(string(portOut), "\n")[0])
	host, port, ok := strings.Cut(addr, ":")
	if !ok {
		t.Fatalf("bad addr %q", addr)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 300*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return "postgres://postgres:smoke-pass@" + net.JoinHostPort(host, port) + "/postgres?sslmode=disable"
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("postgres 未就绪")
	return ""
}

// ER-410：生产 PG 启动必须完成迁移且不创建 root 开发管理员。
func TestProductionPostgresStartsWithoutDevAdmin(t *testing.T) {
	dsn := startPostgres(t)
	t.Setenv("ER_ENV", "production")
	t.Setenv("ER_SECRET", "0123456789abcdef0123456789abcdef")

	var st store.Store
	var err error
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		st, _, err = NewPostgres(context.Background(), dsn)
		if err == nil {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("production postgres start: %v", err)
	}
	if _, err := st.GetAdminByUsername(context.Background(), "root"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("production must not have root admin, err=%v", err)
	}
}
