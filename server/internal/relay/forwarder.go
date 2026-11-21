// Package relay 实现中继数据面：帧解析与双向转发。
// 约束：只转发密文；票据绑定设备身份，未登记来源一律丢弃。
package relay

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/easyremote/server/internal/relaymgr"
)

// 帧类型。
const (
	FrameRegister    byte = 1 // [type][u16 sidLen][sid][u16 devLen][devID][ticket]
	FrameData        byte = 2 // [type][u16 sidLen][sid][payload]
	FrameNodeForward byte = 3 // [type][u16 sidLen][sid][u16 devLen][targetDev][payload]
)

// MaxDatagram 是单个 UDP 包上限。
const MaxDatagram = 64 * 1024

// ErrShort 表示包长度不足。
var ErrShort = errors.New("relay: short frame")

// ParseFrame 解析 [type][u16 sidLen][sid][body]。
// 使用长度前缀而非固定 36 字节，session id 任意长度都能工作。
func ParseFrame(buf []byte) (kind byte, sid string, body []byte, err error) {
	if len(buf) < 3 {
		return 0, "", nil, ErrShort
	}
	kind = buf[0]
	n := int(binary.BigEndian.Uint16(buf[1:3]))
	if len(buf) < 3+n {
		return 0, "", nil, ErrShort
	}
	return kind, string(buf[3 : 3+n]), buf[3+n:], nil
}

// EncodeFrame 构造帧。
func EncodeFrame(kind byte, sid string, body []byte) []byte {
	out := make([]byte, 3+len(sid)+len(body))
	out[0] = kind
	binary.BigEndian.PutUint16(out[1:3], uint16(len(sid)))
	copy(out[3:], sid)
	copy(out[3+len(sid):], body)
	return out
}

// ParseRegister 解析注册体：[u16 devLen][devID][ticket]。
func ParseRegister(body []byte) (deviceID, ticket string, err error) {
	if len(body) < 2 {
		return "", "", ErrShort
	}
	n := int(binary.BigEndian.Uint16(body[0:2]))
	if len(body) < 2+n {
		return "", "", ErrShort
	}
	return string(body[2 : 2+n]), string(body[2+n:]), nil
}

// EncodeRegister 构造注册体。
func EncodeRegister(deviceID, ticket string) []byte {
	out := make([]byte, 2+len(deviceID)+len(ticket))
	binary.BigEndian.PutUint16(out[0:2], uint16(len(deviceID)))
	copy(out[2:], deviceID)
	copy(out[2+len(deviceID):], ticket)
	return out
}

// EncodeNodeForward 构造跨节点转发体：[u16 devLen][devID][payload]。
func EncodeNodeForward(deviceID string, payload []byte) []byte {
	out := make([]byte, 2+len(deviceID)+len(payload))
	binary.BigEndian.PutUint16(out[0:2], uint16(len(deviceID)))
	copy(out[2:], deviceID)
	copy(out[2+len(deviceID):], payload)
	return out
}

// ParseNodeForward 解析跨节点转发体。
func ParseNodeForward(body []byte) (deviceID string, payload []byte, err error) {
	if len(body) < 2 {
		return "", nil, ErrShort
	}
	n := int(binary.BigEndian.Uint16(body[0:2]))
	if len(body) < 2+n {
		return "", nil, ErrShort
	}
	return string(body[2 : 2+n]), body[2+n:], nil
}

type peer struct {
	addr *net.UDPAddr
}

// Pair 是一个会话的端位表：deviceID → 地址。
type Pair struct {
	Peers map[string]*peer
	// PeerOf 记录本节点见过的「设备 → 对端设备」，用于跨节点转发时确定目标。
	PeerOf   map[string]string
	LastSeen time.Time
	Bytes    map[string]int64
}

// Forwarder 维护会话端位并决定转发目标。
type Forwarder struct {
	mu       sync.Mutex
	pairs    map[string]*Pair
	now      func() time.Time
	registry Registry
	nodeID   string
}

// Route 描述一包数据该往哪里去。
type Route struct {
	// Local 非空表示直接投递到本节点的该地址。
	Local net.UDPAddr
	// RemoteNode 非空表示需要转发给另一个中继节点。
	RemoteNode string
}

// SetRegistry 注入跨节点目录；多节点部署必须设置。
func (f *Forwarder) SetRegistry(r Registry, nodeID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registry = r
	f.nodeID = nodeID
}

// NewForwarder 创建转发器。
func NewForwarder() *Forwarder {
	return &Forwarder{pairs: map[string]*Pair{}, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock 仅测试使用。
func (f *Forwarder) SetClock(fn func() time.Time) { f.now = fn }

// Register 校验票据与自称设备一致后登记地址。
// 票据绑定 Self，第三方即使拿到别人的票据也无法占位。
func (f *Forwarder) Register(tk *relaymgr.Ticket, claimedDevice string, addr *net.UDPAddr) bool {
	if tk == nil || claimedDevice == "" || claimedDevice != tk.Self {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.pairs[tk.SessionID]
	if p == nil {
		p = &Pair{Peers: map[string]*peer{}, PeerOf: map[string]string{}, Bytes: map[string]int64{}}
		f.pairs[tk.SessionID] = p
	}
	// 只接受会话双方
	if claimedDevice != tk.Self || (claimedDevice != tk.Peer && tk.Peer == "") {
		return false
	}
	p.Peers[claimedDevice] = &peer{addr: addr}
	p.PeerOf[claimedDevice] = tk.Peer
	p.LastSeen = f.now()
	return true
}

// Plan 返回转发计划：本地对端优先，否则查目录跨节点转发。
func (f *Forwarder) Plan(ctx context.Context, sid string, from *net.UDPAddr) (*Route, error) {
	f.mu.Lock()
	p := f.pairs[sid]
	if p == nil {
		f.mu.Unlock()
		return nil, ErrShort
	}
	fromStr := from.String()
	var sender string
	for dev, pr := range p.Peers {
		if pr.addr.String() == fromStr {
			sender = dev
			break
		}
	}
	if sender == "" {
		f.mu.Unlock()
		return nil, ErrShort
	}
	p.Bytes[sender]++
	p.LastSeen = f.now()
	peerDev := p.PeerOf[sender]
	if peer, ok := p.Peers[peerDev]; ok {
		addr := *peer.addr
		f.mu.Unlock()
		return &Route{Local: addr}, nil
	}
	reg := f.registry
	self := f.nodeID
	f.mu.Unlock()

	if reg == nil {
		return nil, ErrShort
	}
	node, err := reg.LookupRoute(ctx, sid, peerDev)
	if err != nil || node == self {
		return nil, ErrShort
	}
	return &Route{RemoteNode: node}, nil
}

// Target 返回转发目标：必须是已登记设备，且会话恰好两端。
func (f *Forwarder) Target(sid string, from *net.UDPAddr) *net.UDPAddr {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.pairs[sid]
	if p == nil || len(p.Peers) != 2 {
		return nil
	}
	fromStr := from.String()
	var sender string
	for dev, pr := range p.Peers {
		if pr.addr.String() == fromStr {
			sender = dev
			break
		}
	}
	if sender == "" {
		return nil // 陌生地址：丢弃
	}
	p.Bytes[sender]++
	p.LastSeen = f.now()
	for dev, pr := range p.Peers {
		if dev != sender {
			return pr.addr
		}
	}
	return nil
}

// Stats 返回会话统计。
func (f *Forwarder) Stats(sid string) (Pair, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.pairs[sid]
	if !ok {
		return Pair{}, false
	}
	cp := Pair{LastSeen: p.LastSeen, Peers: map[string]*peer{}, Bytes: map[string]int64{}}
	for k, v := range p.Peers {
		cp.Peers[k] = v
	}
	for k, v := range p.Bytes {
		cp.Bytes[k] = v
	}
	return cp, true
}
