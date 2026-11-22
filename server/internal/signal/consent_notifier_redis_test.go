package signal

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type recordingConn struct {
	ch chan Envelope
}

func newRecordingConn() *recordingConn { return &recordingConn{ch: make(chan Envelope, 8)} }
func (c *recordingConn) Send(t int, body any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	c.ch <- Envelope{Type: t, Body: raw}
	return nil
}
func (c *recordingConn) Close() error { return nil }

func startRedis(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatalf("跨实例通知测试需要 docker 启动 Redis: %v", err)
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
	t.Fatalf("redis 未在 20s 内就绪（%s）", addr)
	return ""
}

// TestConsentNotifierCrossesSignalInstances：两个 signal 实例（各自 Hub + RedisBus）
// 分别注册在不同实例上，网关通知器通过共享 Bus 把 CONSENT_REQUIRED 送到目标设备。
func TestConsentNotifierCrossesSignalInstances(t *testing.T) {
	url := startRedis(t)
	ctx := context.Background()

	busA, err := NewRedisBus(ctx, url, "inst-a")
	if err != nil {
		t.Fatal(err)
	}
	defer busA.Close()
	busB, err := NewRedisBus(ctx, url, "inst-b")
	if err != nil {
		t.Fatal(err)
	}
	defer busB.Close()

	hubA := NewHub()
	hubA.SetInstanceID("inst-a")
	hubA.SetBus(busA)
	if err := busA.Subscribe(ctx, hubA.DeliverLocal); err != nil {
		t.Fatal(err)
	}
	hubB := NewHub()
	hubB.SetInstanceID("inst-b")
	hubB.SetBus(busB)
	if err := busB.Subscribe(ctx, hubB.DeliverLocal); err != nil {
		t.Fatal(err)
	}

	connA := newRecordingConn()
	connB := newRecordingConn()
	hubA.Register("dev-a", connA)
	hubB.Register("dev-b", connB)

	// 两个“网关”各自持有自己的 notifier，分别投到对方实例上的设备。
	notifierA := ConsentNotifier{Bus: busA}
	notifierB := ConsentNotifier{Bus: busB}
	if err := notifierA.NotifyConsentRequired("ses-a", "ctrl-a", "dev-b"); err != nil {
		t.Fatal(err)
	}
	if err := notifierB.NotifyConsentRequired("ses-b", "ctrl-b", "dev-a"); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		conn *recordingConn
		sid  string
	}{
		{"A to B", connB, "ses-a"},
		{"B to A", connA, "ses-b"},
	} {
		select {
		case env := <-tc.conn.ch:
			if env.Type != TypeConsentRequired {
				t.Fatalf("%s: type=%d", tc.name, env.Type)
			}
			var body struct {
				SessionID string `json:"session_id"`
				From      string `json:"from"`
			}
			if err := json.Unmarshal(env.Body, &body); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if body.SessionID != tc.sid {
				t.Fatalf("%s: session=%q want %q", tc.name, body.SessionID, tc.sid)
			}
			if body.From == "" {
				t.Fatalf("%s: missing from", tc.name)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: 5s 内未收到跨实例 CONSENT_REQUIRED", tc.name)
		}
	}

	// 绑定必须能被另一个实例读到。
	b, err := busB.LoadBinding(ctx, "ses-a")
	if err != nil {
		t.Fatalf("cross-instance binding: %v", err)
	}
	if b.From != "ctrl-a" || b.To != "dev-b" {
		t.Fatalf("binding=%+v", b)
	}

	// 关闭后：撤销键跨实例可见，旧票据不能再用。
	if err := notifierA.NotifySessionClosed("ses-a", "ctrl-a", "dev-b", "hangup"); err != nil {
		t.Fatal(err)
	}
	if revoked, err := busB.IsRevoked(ctx, "ses-a"); err != nil || !revoked {
		t.Fatalf("revoked=%v err=%v", revoked, err)
	}
	if _, err := busB.LoadBinding(ctx, "ses-a"); err == nil {
		t.Fatal("binding must be deleted on close")
	}
	fmt.Println("cross-instance consent delivered")
}
