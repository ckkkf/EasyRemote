package relay_test

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/easyremote/server/internal/relay"
	"github.com/easyremote/server/internal/relaymgr"
)

// 跨语言兼容回归：Rust 客户端签发的票据与注册帧必须被 Go 中继接受。
// 数据来源：cargo run -p er-session --bin tk_probe 的实际输出。
const rustTicket = "c2VzLWUyZS0xfGRldkF8ZGV2QnwyMDAwMDAwMDAw.xWyRcUu3SZfo9BZNL_m_H9dLJDLwxFWAOY0iSsq-aLY"

// 完整注册帧：[type=1][sidLen][sid][devLen][devID][ticket]
const rustRegisterFrameHex = "0100097365732d6532652d310004646576416332567a4c5755795a5330786647526c646b46385a475632516e77794d4441774d4441774d4441772e7857795263557533535a666f39425a4e4c5f6d5f4839644c4a444c77784657414f5930695373712d614c59"

func TestRustTicketAcceptedByService(t *testing.T) {
	svc := relaymgr.New([]byte("dev-secret-change-me"), time.Hour)
	svc.SetClock(func() time.Time { return time.Unix(1_900_000_000, 0).UTC() })

	tk, err := svc.Verify(rustTicket)
	if err != nil {
		t.Fatalf("Rust ticket rejected: %v", err)
	}
	if tk.SessionID != "ses-e2e-1" || tk.Self != "devA" || tk.Peer != "devB" {
		t.Fatalf("field mismatch: %+v", tk)
	}
}

func TestRustRegisterFrameParses(t *testing.T) {
	raw, err := hex.DecodeString(rustRegisterFrameHex)
	if err != nil {
		t.Fatal(err)
	}
	kind, sid, body, err := relay.ParseFrame(raw)
	if err != nil {
		t.Fatalf("parse frame: %v", err)
	}
	if kind != relay.FrameRegister {
		t.Fatalf("kind = %d", kind)
	}
	if sid != "ses-e2e-1" {
		t.Fatalf("sid = %q", sid)
	}
	dev, tk, err := relay.ParseRegister(body)
	if err != nil {
		t.Fatalf("parse register: %v", err)
	}
	if dev != "devA" || tk != rustTicket {
		t.Fatalf("register mismatch: %q", dev)
	}
}

func TestRustTicketPayloadLayout(t *testing.T) {
	body := strings.SplitN(rustTicket, ".", 2)[0]
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "ses-e2e-1|devA|devB|2000000000" {
		t.Fatalf("payload = %q", string(raw))
	}
}

// 真实转发：两个设备注册后，Go 中继必须把 A 的数据指向 B。
func TestForwarderRoutesRegisteredPair(t *testing.T) {
	svc := relaymgr.New([]byte("dev-secret-change-me"), time.Hour)
	fwd := relay.NewForwarder()

	addrA, err := net.ResolveUDPAddr("udp", "127.0.0.1:50001")
	if err != nil {
		t.Fatal(err)
	}
	addrB, err := net.ResolveUDPAddr("udp", "127.0.0.1:50002")
	if err != nil {
		t.Fatal(err)
	}

	_, tkA, err := svc.Issue("ses-e2e-1", "devA", "devB")
	if err != nil {
		t.Fatal(err)
	}
	_, tkB, err := svc.Issue("ses-e2e-1", "devB", "devA")
	if err != nil {
		t.Fatal(err)
	}
	if !fwd.Register(tkA, "devA", addrA) {
		t.Fatal("register A failed")
	}
	if !fwd.Register(tkB, "devB", addrB) {
		t.Fatal("register B failed")
	}

	plan, err := fwd.Plan(context.Background(), "ses-e2e-1", addrA)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Local.String() != "127.0.0.1:50002" {
		t.Fatalf("target = %v", plan.Local.String())
	}
}
