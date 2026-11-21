package signal

import (
	"context"
	"os"
	"testing"
	"time"
)

// S5 回归：同一设备重连后，旧连接 Unregister 不得清掉新连接的在线标记。
func TestStaleUnregisterKeepsNewPresence(t *testing.T) {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		t.Skip("REDIS_URL not set")
	}
	ctx := context.Background()
	bus, err := NewRedisBus(ctx, url, "inst-x")
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()

	h := NewHub()
	h.SetInstanceID("inst-x")
	h.SetBus(bus)

	oldConn := &fakeConn{}
	newConn := &fakeConn{}
	h.Register("dev-z", oldConn)
	h.Register("dev-z", newConn) // 重连

	// 旧连接关闭（不应影响新连接）
	if h.Unregister("dev-z", oldConn) {
		t.Fatal("stale conn must not unregister")
	}
	if ok, _ := bus.IsOnline(ctx, "dev-z"); !ok {
		t.Fatal("presence must survive stale close")
	}
	// 新连接关闭才清除
	if !h.Unregister("dev-z", newConn) {
		t.Fatal("owner conn should unregister")
	}
	if ok, _ := bus.IsOnline(ctx, "dev-z"); ok {
		t.Fatal("presence should be cleared after owner close")
	}
}

// S3 回归：共享绑定必须带 TTL。
func TestBindingHasTTL(t *testing.T) {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		t.Skip("REDIS_URL not set")
	}
	ctx := context.Background()
	bus, err := NewRedisBus(ctx, url, "inst-y")
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()

	b := &Binding{SessionID: "ses-ttl-1", From: "a", To: "b", CreatedAt: time.Now().UTC()}
	if err := bus.SaveBinding(ctx, b, 90*time.Second); err != nil {
		t.Fatal(err)
	}
	key := bindingKey(b.SessionID)
	ttl := bus.rdb.PTTL(ctx, key).Val()
	if ttl <= 0 {
		t.Fatalf("binding must have TTL, got %v", ttl)
	}
	_ = bus.DeleteBinding(ctx, b.SessionID)
}
