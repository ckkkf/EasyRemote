package relay

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/easyremote/server/internal/relaymgr"
)

func addrOf(t *testing.T, s string) *net.UDPAddr {
	t.Helper()
	a, err := net.ResolveUDPAddr("udp", s)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestPlanPrefersLocalPeer(t *testing.T) {
	f := NewForwarder()
	a := addrOf(t, "127.0.0.1:20001")
	b := addrOf(t, "127.0.0.1:20002")
	f.Register(&relaymgr.Ticket{SessionID: "s1", Self: "devA", Peer: "devB"}, "devA", a)
	f.Register(&relaymgr.Ticket{SessionID: "s1", Self: "devB", Peer: "devA"}, "devB", b)

	plan, err := f.Plan(context.Background(), "s1", a)
	if err != nil {
		t.Fatal(err)
	}
	if plan.RemoteNode != "" {
		t.Fatal("local peer should not go remote")
	}
	if plan.Local.String() != b.String() {
		t.Fatalf("local target = %v", plan.Local.String())
	}
}

func TestPlanFallsBackToRegistry(t *testing.T) {
	reg := NewMemoryRegistry()
	ctx := context.Background()
	_ = reg.RegisterNode(ctx, "node-2", "127.0.0.1:19102", time.Minute)
	_ = reg.SetRoute(ctx, "s1", "devB", "node-2", time.Minute)

	f := NewForwarder()
	f.SetRegistry(reg, "node-1")
	a := addrOf(t, "127.0.0.1:20001")
	// 只有 A 注册在本节点，B 在 node-2
	f.Register(&relaymgr.Ticket{SessionID: "s1", Self: "devA", Peer: "devB"}, "devA", a)

	plan, err := f.Plan(ctx, "s1", a)
	if err != nil {
		t.Fatal(err)
	}
	if plan.RemoteNode != "node-2" {
		t.Fatalf("want remote node-2, got %q", plan.RemoteNode)
	}
}

func TestPlanRejectsStrangerEvenWithRegistry(t *testing.T) {
	reg := NewMemoryRegistry()
	ctx := context.Background()
	_ = reg.SetRoute(ctx, "s1", "devB", "node-2", time.Minute)

	f := NewForwarder()
	f.SetRegistry(reg, "node-1")
	a := addrOf(t, "127.0.0.1:20001")
	f.Register(&relaymgr.Ticket{SessionID: "s1", Self: "devA", Peer: "devB"}, "devA", a)

	if _, err := f.Plan(ctx, "s1", addrOf(t, "127.0.0.1:29999")); err == nil {
		t.Fatal("stranger must be rejected even when registry has a route")
	}
}

func TestNodeForwardCodec(t *testing.T) {
	payload := []byte("ciphertext")
	body := EncodeNodeForward("devB", payload)
	dev, got, err := ParseNodeForward(body)
	if err != nil || dev != "devB" || string(got) != "ciphertext" {
		t.Fatalf("codec mismatch: %q %q %v", dev, got, err)
	}
	if _, _, err := ParseNodeForward([]byte{0}); err != ErrShort {
		t.Fatal("short body must error")
	}
}
