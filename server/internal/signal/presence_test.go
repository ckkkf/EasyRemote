package signal

import (
	"context"
	"sync"
	"testing"
	"time"
)

// ttlBus 用可控时钟模拟 Redis 的 TTL 在线表。
type ttlBus struct {
	mu     sync.Mutex
	now    time.Time
	online map[string]struct {
		token   string
		expires time.Time
	}
}

func newTTLBus(now time.Time) *ttlBus {
	return &ttlBus{now: now, online: map[string]struct {
		token   string
		expires time.Time
	}{}}
}

func (b *ttlBus) advance(d time.Duration) { b.mu.Lock(); b.now = b.now.Add(d); b.mu.Unlock() }

func (b *ttlBus) Publish(context.Context, string, Envelope) error               { return nil }
func (b *ttlBus) Subscribe(context.Context, func(string, Envelope) error) error { return nil }
func (b *ttlBus) SaveBinding(context.Context, *Binding, time.Duration) error    { return nil }
func (b *ttlBus) LoadBinding(context.Context, string) (*Binding, error)         { return nil, ErrNotBound }
func (b *ttlBus) DeleteBinding(context.Context, string) error                   { return nil }
func (b *ttlBus) SetOnline(_ context.Context, deviceID, token string, ttl time.Duration) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.online[deviceID] = struct {
		token   string
		expires time.Time
	}{token, b.now.Add(ttl)}
	return nil
}
func (b *ttlBus) ClearOnline(_ context.Context, deviceID, instance string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if cur, ok := b.online[deviceID]; ok && cur.token == instance {
		delete(b.online, deviceID)
	}
	return nil
}
func (b *ttlBus) IsOnline(_ context.Context, deviceID string) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	cur, ok := b.online[deviceID]
	if !ok || !b.now.Before(cur.expires) {
		return false, nil
	}
	return true, nil
}
func (b *ttlBus) Close() error { return nil }

type presenceConn struct{}

func (*presenceConn) Send(int, any) error { return nil }
func (*presenceConn) Close() error        { return nil }

// TestPresenceRefreshesOnHeartbeat：心跳必须续租 TTL；否则 5 分钟后在线设备被误判离线。
func TestPresenceRefreshesOnHeartbeat(t *testing.T) {
	bus := newTTLBus(time.Now())
	hub := NewHub()
	hub.SetInstanceID("inst-a")
	hub.SetBus(bus)
	conn := &presenceConn{}
	hub.Register("dev-a", conn)

	bus.advance(4 * time.Minute)
	if ok, _ := bus.IsOnline(context.Background(), "dev-a"); !ok {
		t.Fatal("should still be online before TTL")
	}
	if !hub.Touch("dev-a", conn) {
		t.Fatal("touch rejected")
	}
	bus.advance(4 * time.Minute)
	if ok, _ := bus.IsOnline(context.Background(), "dev-a"); !ok {
		t.Fatal("heartbeat must renew presence TTL")
	}
}

// TestDisconnectOffline：主动断开必须撤销在线标记。
func TestDisconnectOffline(t *testing.T) {
	bus := newTTLBus(time.Now())
	hub := NewHub()
	hub.SetInstanceID("inst-a")
	hub.SetBus(bus)
	conn := &presenceConn{}
	hub.Register("dev-b", conn)
	if !hub.Unregister("dev-b", conn) {
		t.Fatal("unregister failed")
	}
	if ok, _ := bus.IsOnline(context.Background(), "dev-b"); ok {
		t.Fatal("disconnect must clear presence")
	}
}

// TestCrashExpires：进程崩溃后没有心跳，TTL 到期必须自动离线。
func TestCrashExpires(t *testing.T) {
	bus := newTTLBus(time.Now())
	hub := NewHub()
	hub.SetInstanceID("inst-a")
	hub.SetBus(bus)
	hub.Register("dev-c", &presenceConn{})

	bus.advance(presenceTTL + time.Second)
	if ok, _ := bus.IsOnline(context.Background(), "dev-c"); ok {
		t.Fatal("presence must expire after TTL without heartbeat")
	}
}

// 非当前连接不能续租，避免旧连接把新连接踢下线。
func TestTouchRejectsStaleConn(t *testing.T) {
	hub := NewHub()
	conn := &presenceConn{}
	hub.Register("dev-d", conn)
	if hub.Touch("dev-d", &presenceConn{}) {
		t.Fatal("stale conn must not renew presence")
	}
}
