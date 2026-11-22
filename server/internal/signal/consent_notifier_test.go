package signal

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type fakeBus struct {
	saved     *Binding
	revoked   []string
	published []struct {
		device string
		msg    Envelope
	}
}

func (f *fakeBus) RevokeSession(_ context.Context, sessionID string, _ time.Duration) error {
	f.revoked = append(f.revoked, sessionID)
	return nil
}

func (f *fakeBus) Publish(_ context.Context, deviceID string, msg Envelope) error {
	f.published = append(f.published, struct {
		device string
		msg    Envelope
	}{deviceID, msg})
	return nil
}
func (f *fakeBus) Subscribe(context.Context, func(string, Envelope) error) error { return nil }
func (f *fakeBus) SaveBinding(_ context.Context, b *Binding, _ time.Duration) error {
	f.saved = b
	return nil
}
func (f *fakeBus) LoadBinding(context.Context, string) (*Binding, error)          { return f.saved, nil }
func (f *fakeBus) DeleteBinding(context.Context, string) error                    { return nil }
func (f *fakeBus) SetOnline(context.Context, string, string, time.Duration) error { return nil }
func (f *fakeBus) ClearOnline(context.Context, string, string) error              { return nil }
func (f *fakeBus) IsOnline(context.Context, string) (bool, error)                 { return true, nil }
func (f *fakeBus) Close() error                                                   { return nil }

// 关闭必须同时撤销旧票据（共享撤销键）并通知双方。
func TestConsentNotifierRevokesOnClose(t *testing.T) {
	bus := &fakeBus{}
	n := ConsentNotifier{Bus: bus}
	if err := n.NotifySessionClosed("ses-close", "ctrl", "target", "hangup"); err != nil {
		t.Fatal(err)
	}
	if len(bus.published) != 2 {
		t.Fatalf("published=%d, want both peers", len(bus.published))
	}
	for _, p := range bus.published {
		if p.msg.Type != TypeDisconnect {
			t.Fatalf("type=%d, want disconnect", p.msg.Type)
		}
	}
	if len(bus.revoked) != 1 || bus.revoked[0] != "ses-close" {
		t.Fatalf("revoked=%v", bus.revoked)
	}
}

// TestConsentNotifierUsesBus：网关通知器必须写跨实例绑定并通过 Bus 投递，
// 不能依赖进程内 Hub。
func TestConsentNotifierUsesBus(t *testing.T) {
	bus := &fakeBus{}
	n := ConsentNotifier{Bus: bus}
	if err := n.NotifyConsentRequired("ses-1", "ctrl", "target"); err != nil {
		t.Fatal(err)
	}
	if bus.saved == nil || bus.saved.SessionID != "ses-1" || bus.saved.From != "ctrl" || bus.saved.To != "target" {
		t.Fatalf("binding not saved: %+v", bus.saved)
	}
	if len(bus.published) != 1 {
		t.Fatalf("published=%d, want 1", len(bus.published))
	}
	got := bus.published[0]
	if got.device != "target" || got.msg.Type != TypeConsentRequired {
		t.Fatalf("publish target/type wrong: %+v", got)
	}
	var body struct {
		SessionID string `json:"session_id"`
		From      string `json:"from"`
	}
	if err := json.Unmarshal(got.msg.Body, &body); err != nil {
		t.Fatal(err)
	}
	if body.SessionID != "ses-1" || body.From != "ctrl" {
		t.Fatalf("body=%+v", body)
	}
}
