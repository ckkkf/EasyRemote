// Package netdiag 提供网络诊断的最小基础设施：STUN 反射服务（RFC 5389）。
//
// 为什么自己实现而不是引第三方库：
//   - 我们只需要 Binding Request/Response 这一对报文，完整 ICE 栈用不上；
//   - 依赖越少，中继与 NAT 探测链路越容易审计。
//
// 实现范围（明确边界，避免被当成完整 STUN 实现误用）：
//   - 只处理 IPv4/IPv6 的 UDP Binding Request（0x0001）；
//   - 只回 XOR-MAPPED-ADDRESS（0x0020）与可选的 SOFTWARE（0x8022）；
//   - 不实现认证、不实现 TURN 的 Allocate/Channel 语义。
package netdiag

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"
)

const (
	// magicCookie 是 RFC 5389 §6 规定的固定值，用来区分 STUN 与旧版 RFC 3489 报文。
	magicCookie uint32 = 0x2112A442
	// headerLen = type(2) + length(2) + cookie(4) + transaction id(12)
	headerLen = 20

	typeBindingRequest = 0x0001
	typeBindingSuccess = 0x0101
	typeBindingError   = 0x0111

	attrMappedAddress    = 0x0001
	attrXorMappedAddress = 0x0020
	attrSoftware         = 0x8022

	familyIPv4 = 0x01
	familyIPv6 = 0x02

	// maxMessage 是单条 UDP 报文上限（够 IPv6 头部 + 少量属性）。
	maxMessage = 1500
)

// ErrNotSTUN 表示收到的报文不是合法的 STUN 消息。
var ErrNotSTUN = errors.New("netdiag: not a stun message")

// Addr 是 STUN 属性里的地址（IP:port）。
type Addr struct {
	IP   net.IP
	Port int
}

// String 便于日志与测试断言。
func (a Addr) String() string {
	if a.IP == nil {
		return ""
	}
	return net.JoinHostPort(a.IP.String(), fmt.Sprintf("%d", a.Port))
}

// Equal 比较地址是否相同。
func (a Addr) Equal(b Addr) bool {
	return a.Port == b.Port && a.IP.Equal(b.IP)
}

// Message 是本项目需要的那部分 STUN 消息。
type Message struct {
	Type          uint16
	TransactionID [12]byte
	XORMapped     *Addr
	Mapped        *Addr
}

// xorAddr 按 RFC 5389 §15.2 把地址与 magic cookie（IPv6 还要拼上 transaction id）异或。
func xorAddr(a Addr, tid [12]byte) (net.IP, int) {
	ip := a.IP.To4()
	port := a.Port ^ int(magicCookie>>16)
	if ip != nil {
		var cookie [4]byte
		binary.BigEndian.PutUint32(cookie[:], magicCookie)
		out := make(net.IP, 4)
		for i := 0; i < 4; i++ {
			out[i] = ip[i] ^ cookie[i]
		}
		return out, port
	}
	ip16 := a.IP.To16()
	if ip16 == nil {
		return nil, 0
	}
	var mask [16]byte
	binary.BigEndian.PutUint32(mask[0:4], magicCookie)
	copy(mask[4:], tid[:])
	out := make(net.IP, 16)
	for i := 0; i < 16; i++ {
		out[i] = ip16[i] ^ mask[i]
	}
	return out, port
}

// unxorAddr 是 xorAddr 的逆运算。
func unxorAddr(raw net.IP, port int, tid [12]byte) Addr {
	p := port ^ int(magicCookie>>16)
	if v4 := raw.To4(); v4 != nil {
		var cookie [4]byte
		binary.BigEndian.PutUint32(cookie[:], magicCookie)
		out := make(net.IP, 4)
		for i := 0; i < 4; i++ {
			out[i] = v4[i] ^ cookie[i]
		}
		return Addr{IP: out, Port: p}
	}
	var mask [16]byte
	binary.BigEndian.PutUint32(mask[0:4], magicCookie)
	copy(mask[4:], tid[:])
	ip16 := raw.To16()
	out := make(net.IP, 16)
	for i := 0; i < 16; i++ {
		out[i] = ip16[i] ^ mask[i]
	}
	return Addr{IP: out, Port: p}
}

// encodeAttrAddress 编码 XOR-MAPPED-ADDRESS / MAPPED-ADDRESS 属性体。
func encodeAddrAttrValue(a Addr, xored bool, tid [12]byte) []byte {
	ip := a.IP.To4()
	family := byte(familyIPv4)
	port := a.Port
	if ip == nil {
		ip = a.IP.To16()
		family = familyIPv6
	}
	if xored {
		ip, port = xorAddr(a, tid)
	}
	buf := make([]byte, 4+len(ip))
	buf[0] = 0
	buf[1] = family
	binary.BigEndian.PutUint16(buf[2:4], uint16(port))
	copy(buf[4:], ip)
	return buf
}

func appendAttr(dst []byte, attrType uint16, value []byte) []byte {
	var hdr [4]byte
	binary.BigEndian.PutUint16(hdr[0:2], attrType)
	binary.BigEndian.PutUint16(hdr[2:4], uint16(len(value)))
	dst = append(dst, hdr[:]...)
	dst = append(dst, value...)
	// 属性按 4 字节对齐填充。
	for len(value)%4 != 0 {
		dst = append(dst, 0)
		value = append(value, 0)
	}
	return dst
}

// buildBindingSuccess 组装 Binding Success Response。
func buildBindingSuccess(tid [12]byte, observed Addr, software string) []byte {
	body := make([]byte, 0, 64)
	body = appendAttr(body, attrXorMappedAddress, encodeAddrAttrValue(observed, true, tid))
	body = appendAttr(body, attrMappedAddress, encodeAddrAttrValue(observed, false, tid))
	if software != "" {
		body = appendAttr(body, attrSoftware, []byte(software))
	}
	msg := make([]byte, headerLen, headerLen+len(body))
	binary.BigEndian.PutUint16(msg[0:2], typeBindingSuccess)
	binary.BigEndian.PutUint16(msg[2:4], uint16(len(body)))
	binary.BigEndian.PutUint32(msg[4:8], magicCookie)
	copy(msg[8:20], tid[:])
	msg = append(msg, body...)
	return msg
}

// Parse 解析一条 STUN 消息（只认 Binding Request/Response 与地址属性）。
func Parse(b []byte) (*Message, error) {
	if len(b) < headerLen {
		return nil, ErrNotSTUN
	}
	msgType := binary.BigEndian.Uint16(b[0:2])
	// 前两位必须为 0（RFC 5389 §6）。
	if msgType&0xC000 != 0 {
		return nil, ErrNotSTUN
	}
	length := int(binary.BigEndian.Uint16(b[2:4]))
	if length%4 != 0 || headerLen+length > len(b) {
		return nil, ErrNotSTUN
	}
	if binary.BigEndian.Uint32(b[4:8]) != magicCookie {
		return nil, ErrNotSTUN
	}
	m := &Message{Type: msgType}
	copy(m.TransactionID[:], b[8:20])

	off := headerLen
	end := headerLen + length
	for off+4 <= end {
		attrType := binary.BigEndian.Uint16(b[off : off+2])
		attrLen := int(binary.BigEndian.Uint16(b[off+2 : off+4]))
		off += 4
		if off+attrLen > end {
			return nil, ErrNotSTUN
		}
		value := b[off : off+attrLen]
		if attrType == attrXorMappedAddress && attrLen >= 8 {
			addr, err := parseAddrValue(value, true, m.TransactionID)
			if err != nil {
				return nil, err
			}
			m.XORMapped = &addr
		}
		if attrType == attrMappedAddress && attrLen >= 8 && m.Mapped == nil {
			addr, err := parseAddrValue(value, false, m.TransactionID)
			if err != nil {
				return nil, err
			}
			m.Mapped = &addr
		}
		// 属性按 4 字节对齐。
		off += attrLen
		for attrLen%4 != 0 {
			off++
			attrLen++
		}
	}
	return m, nil
}

func parseAddrValue(value []byte, xored bool, tid [12]byte) (Addr, error) {
	family := value[1]
	port := int(binary.BigEndian.Uint16(value[2:4]))
	raw := net.IP(value[4:])
	switch family {
	case familyIPv4:
		if len(raw) < 4 {
			return Addr{}, ErrNotSTUN
		}
		raw = raw[:4]
	case familyIPv6:
		if len(raw) < 16 {
			return Addr{}, ErrNotSTUN
		}
		raw = raw[:16]
	default:
		return Addr{}, ErrNotSTUN
	}
	if xored {
		return unxorAddr(raw, port, tid), nil
	}
	return Addr{IP: raw, Port: port}, nil
}

// BuildBindingRequest 生成一条 Binding Request（客户端用）。
func BuildBindingRequest(tid [12]byte) []byte {
	msg := make([]byte, headerLen)
	binary.BigEndian.PutUint16(msg[0:2], typeBindingRequest)
	binary.BigEndian.PutUint16(msg[2:4], 0)
	binary.BigEndian.PutUint32(msg[4:8], magicCookie)
	copy(msg[8:20], tid[:])
	return msg
}

// Server 是最小 STUN 反射服务。
type Server struct {
	conn     net.UDPConn
	software string
	closed   chan struct{}
}

// Listen 在 addr 上监听 UDP（如 ":3478" 或 "127.0.0.1:0"）。
func Listen(addr string) (*Server, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}
	return &Server{conn: *conn, software: "EasyRemote-STUN/1", closed: make(chan struct{})}, nil
}

// Addr 返回实际监听地址（:0 时用得上）。
func (s *Server) Addr() net.Addr { return s.conn.LocalAddr() }

// Close 关闭监听。
func (s *Server) Close() error {
	select {
	case <-s.closed:
		return nil
	default:
		close(s.closed)
	}
	return s.conn.Close()
}

// Serve 处理请求直到 ctx 取消或 Close。
func (s *Server) Serve(ctx context.Context) error {
	buf := make([]byte, maxMessage)
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		// 用短读超时轮询 ctx，避免 ctx 取消后仍阻塞在 ReadFromUDP。
		_ = s.conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, from, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			select {
			case <-s.closed:
				return nil
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		msg, perr := Parse(buf[:n])
		if perr != nil || msg.Type != typeBindingRequest {
			// 非 STUN 或非 Binding Request：静默丢弃，不放大流量。
			continue
		}
		resp := buildBindingSuccess(msg.TransactionID, Addr{IP: from.IP, Port: from.Port}, s.software)
		_, _ = s.conn.WriteToUDP(resp, from)
	}
}

// Result 是一次 STUN 探测的结果。
type Result struct {
	// Observed 是服务器观察到的本端地址（NAT 外侧看到的映射地址）。
	Observed Addr
	// Local 是发送探测的本机套接字地址（用于判断是否直连/同网段）。
	Local Addr
}

// Reflect 向 stunAddr 发送 Binding Request 并解析 XOR-MAPPED-ADDRESS。
func Reflect(ctx context.Context, stunAddr string) (Result, error) {
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "udp", stunAddr)
	if err != nil {
		return Result{}, err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	}

	var tid [12]byte
	copy(tid[:], []byte(time.Now().UTC().Format("200601021504.000")))
	if _, err := conn.Write(BuildBindingRequest(tid)); err != nil {
		return Result{}, err
	}
	buf := make([]byte, maxMessage)
	n, err := conn.Read(buf)
	if err != nil {
		return Result{}, err
	}
	msg, err := Parse(buf[:n])
	if err != nil {
		return Result{}, err
	}
	if msg.Type != typeBindingSuccess {
		return Result{}, fmt.Errorf("netdiag: unexpected stun type 0x%04x", msg.Type)
	}
	if msg.XORMapped == nil {
		return Result{}, errors.New("netdiag: response without XOR-MAPPED-ADDRESS")
	}
	res := Result{Observed: *msg.XORMapped}
	if local, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		res.Local = Addr{IP: local.IP, Port: local.Port}
	}
	return res, nil
}
