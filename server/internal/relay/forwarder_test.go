package relay

import (
	"net"
	"testing"

	"github.com/easyremote/server/internal/relaymgr"
)

func attr(t *testing.T, s string) *net.UDPAddr {
	t.Helper()
	a, err := net.ResolveUDPAddr("udp", s)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestFrameRoundtripAnySessionID(t *testing.T) {
	for _, sid := range []string{"ses-235900-7", "a", "0123456789012345678901234567890123456"} {
		frame := EncodeFrame(FrameData, sid, []byte("payload"))
		kind, got, body, err := ParseFrame(frame)
		if err != nil {
			t.Fatalf("parse %q: %v", sid, err)
		}
		if kind != FrameData || got != sid || string(body) != "payload" {
			t.Fatalf("mismatch: %d %q %q", kind, got, body)
		}
	}
}

func TestRegisterRoundtrip(t *testing.T) {
	body := EncodeRegister("devA", "ticket-xyz")
	dev, tk, err := ParseRegister(body)
	if err != nil || dev != "devA" || tk != "ticket-xyz" {
		t.Fatalf("got %q %q %v", dev, tk, err)
	}
}

func TestParseShortFrame(t *testing.T) {
	if _, _, _, err := ParseFrame([]byte{1}); err != ErrShort {
		t.Fatal("want ErrShort")
	}
}

func TestTicketMustMatchClaimedDevice(t *testing.T) {
	f := NewForwarder()
	tkA := &relaymgr.Ticket{SessionID: "s1", Self: "devA", Peer: "devB"}
	if f.Register(tkA, "devEVIL", attr(t, "127.0.0.1:1003")) {
		t.Fatal("ticket used by another device must be rejected")
	}
	if !f.Register(tkA, "devA", attr(t, "127.0.0.1:1001")) {
		t.Fatal("legit register failed")
	}
}

func TestStrangerCannotInject(t *testing.T) {
	f := NewForwarder()
	a := attr(t, "127.0.0.1:1001")
	b := attr(t, "127.0.0.1:1002")
	f.Register(&relaymgr.Ticket{SessionID: "s1", Self: "devA", Peer: "devB"}, "devA", a)
	f.Register(&relaymgr.Ticket{SessionID: "s1", Self: "devB", Peer: "devA"}, "devB", b)

	if got := f.Target("s1", attr(t, "127.0.0.1:9999")); got != nil {
		t.Fatalf("stranger must not be routed, got %v", got)
	}
}

func TestThirdPartyCannotTakeSlot(t *testing.T) {
	f := NewForwarder()
	a := attr(t, "127.0.0.1:1001")
	b := attr(t, "127.0.0.1:1002")
	f.Register(&relaymgr.Ticket{SessionID: "s1", Self: "devA", Peer: "devB"}, "devA", a)
	f.Register(&relaymgr.Ticket{SessionID: "s1", Self: "devB", Peer: "devA"}, "devB", b)

	// 第三方拿 B 的票据但自称 B，地址不同：允许（B 换网），但不得抢走 A 的位置
	f.Register(&relaymgr.Ticket{SessionID: "s1", Self: "devB", Peer: "devA"}, "devB", attr(t, "127.0.0.1:1003"))
	got := f.Target("s1", a)
	if got == nil || got.String() != "127.0.0.1:1003" {
		t.Fatalf("B reconnect should update target, got %v", got)
	}
	// A 的位置不可被占用
	if f.Register(&relaymgr.Ticket{SessionID: "s1", Self: "devB", Peer: "devA"}, "devA", attr(t, "127.0.0.1:1004")) {
		t.Fatal("B's ticket must not be usable as devA")
	}
}

func TestBidirectionalAndStats(t *testing.T) {
	f := NewForwarder()
	a := attr(t, "127.0.0.1:1001")
	b := attr(t, "127.0.0.1:1002")
	f.Register(&relaymgr.Ticket{SessionID: "s1", Self: "devA", Peer: "devB"}, "devA", a)
	f.Register(&relaymgr.Ticket{SessionID: "s1", Self: "devB", Peer: "devA"}, "devB", b)

	if got := f.Target("s1", a); got.String() != b.String() {
		t.Fatalf("a→b wrong: %v", got)
	}
	if got := f.Target("s1", b); got.String() != a.String() {
		t.Fatalf("b→a wrong: %v", got)
	}
	st, _ := f.Stats("s1")
	if st.Bytes["devA"] != 1 || st.Bytes["devB"] != 1 {
		t.Fatalf("stats wrong: %+v", st.Bytes)
	}
}

func TestNoForwardUntilBothRegistered(t *testing.T) {
	f := NewForwarder()
	a := attr(t, "127.0.0.1:1001")
	f.Register(&relaymgr.Ticket{SessionID: "s1", Self: "devA", Peer: "devB"}, "devA", a)
	if got := f.Target("s1", a); got != nil {
		t.Fatalf("should not forward with one peer, got %v", got)
	}
}
