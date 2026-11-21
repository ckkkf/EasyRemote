package netdiag

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// TestBindingRoundTripLocalUDP 在本地 UDP 回环上跑一遍完整往返：
// 服务端观察到的地址必须等于客户端套接字的真实地址（回环上没有 NAT，二者相等）。
func TestBindingRoundTripLocalUDP(t *testing.T) {
	srv, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()

	probeCtx, probeCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer probeCancel()
	res, err := Reflect(probeCtx, srv.Addr().String())
	if err != nil {
		t.Fatalf("reflect: %v", err)
	}
	if res.Observed.Port == 0 || res.Observed.IP == nil {
		t.Fatalf("observed address empty: %+v", res.Observed)
	}
	if !res.Observed.IP.IsLoopback() {
		t.Fatalf("want loopback observed, got %s", res.Observed)
	}
	if res.Observed.Port != res.Local.Port {
		t.Fatalf("observed port %d != local port %d", res.Observed.Port, res.Local.Port)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not stop on context cancel")
	}
}

// TestXorMappedAddressRoundTrip 校验 XOR 编解码互逆（IPv4 与 IPv6）。
func TestXorMappedAddressRoundTrip(t *testing.T) {
	var tid [12]byte
	for i := range tid {
		tid[i] = byte(i)
	}
	cases := []Addr{
		{IP: net.ParseIP("203.0.113.7"), Port: 51820},
		{IP: net.ParseIP("192.168.1.10"), Port: 1},
		{IP: net.ParseIP("2001:db8::1"), Port: 65535},
	}
	for _, want := range cases {
		raw, port := xorAddr(want, tid)
		if got := unxorAddr(raw, port, tid); !got.Equal(want) {
			t.Fatalf("xor round trip: want %s got %s", want, got)
		}
	}
}

// TestParseRejectsGarbage 确保非 STUN 报文被拒绝，而不是被当成请求处理。
func TestParseRejectsGarbage(t *testing.T) {
	cases := [][]byte{
		nil,
		[]byte("hello"),
		make([]byte, headerLen), // magic cookie 为 0
		func() []byte {
			b := BuildBindingRequest([12]byte{})
			b[0] = 0xC0 // 前两位必须为 0
			return b
		}(),
	}
	for i, c := range cases {
		if _, err := Parse(c); !errors.Is(err, ErrNotSTUN) {
			t.Fatalf("case %d: want ErrNotSTUN, got %v", i, err)
		}
	}
}

// TestParseBindingRequest 校验请求报文能被正确识别并保留 transaction id。
func TestParseBindingRequest(t *testing.T) {
	var tid [12]byte
	for i := range tid {
		tid[i] = byte(0xA0 + i)
	}
	msg, err := Parse(BuildBindingRequest(tid))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if msg.Type != typeBindingRequest {
		t.Fatalf("type = 0x%04x", msg.Type)
	}
	if msg.TransactionID != tid {
		t.Fatalf("transaction id mismatch")
	}
}

// TestServerIgnoresJunk 确认服务端对垃圾报文静默丢弃且仍能继续服务。
func TestServerIgnoresJunk(t *testing.T) {
	srv, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()

	conn, err := net.Dial("udp", srv.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("definitely-not-stun")); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()

	probeCtx, probeCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer probeCancel()
	if _, err := Reflect(probeCtx, srv.Addr().String()); err != nil {
		t.Fatalf("server stopped serving after junk packet: %v", err)
	}
}
