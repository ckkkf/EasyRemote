package signal

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestRedisBusPubSub(t *testing.T) {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		t.Skip("REDIS_URL not set")
	}
	ctx := context.Background()
	bus, err := NewRedisBus(ctx, url, "inst-test")
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()

	got := make(chan Envelope, 1)
	if err := bus.Subscribe(ctx, func(deviceID string, msg Envelope) error {
		if deviceID == "dev-bus" {
			got <- msg
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"session_id": "s1"})
	time.Sleep(100 * time.Millisecond) // 等订阅生效
	if err := bus.Publish(ctx, "dev-bus", Envelope{Type: TypeCandidates, Body: body}); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-got:
		if m.Type != TypeCandidates {
			t.Fatalf("type = %d", m.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("message not delivered across instances")
	}
}

func TestRedisPresenceLifecycle(t *testing.T) {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		t.Skip("REDIS_URL not set")
	}
	ctx := context.Background()
	bus, err := NewRedisBus(ctx, url, "inst-a")
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()

	if err := bus.SetOnline(ctx, "dev-a", "inst-a", time.Minute); err != nil {
		t.Fatal(err)
	}
	if ok, _ := bus.IsOnline(ctx, "dev-a"); !ok {
		t.Fatal("should be online")
	}
	// 另一个实例不能清掉本实例的在线标记
	if err := bus.ClearOnline(ctx, "dev-a", "inst-b"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := bus.IsOnline(ctx, "dev-a"); !ok {
		t.Fatal("other instance must not clear presence")
	}
	if err := bus.ClearOnline(ctx, "dev-a", "inst-a"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := bus.IsOnline(ctx, "dev-a"); ok {
		t.Fatal("should be offline after clear")
	}
}
