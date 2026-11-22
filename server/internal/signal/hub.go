// Package signal 实现信令服务的转发逻辑：在线表、会话绑定与消息路由。
// cmd/rendezvous 只做装配；本包不依赖 websocket，便于单元测试。
package signal

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// 消息类型，与 docs/spec/protocol.md §4.1 对齐。
const (
	TypeHello           = 0x01
	TypeHelloAck        = 0x02
	TypeQueryDevice     = 0x03
	TypeDeviceStatus    = 0x04
	TypeRequestConnect  = 0x05
	TypeConsentRequired = 0x07
	TypeConsentResult   = 0x09
	TypeConsentGranted  = 0x0A
	TypeCandidates      = 0x0B
	TypeDisconnect      = 0x0C
	// TypeHeartbeat 由客户端在上层读超时后发送；仅用于续租 presence。
	TypeHeartbeat = 0x7F
)

// bindingTTL 是共享绑定的存活时间；presenceTTL 是在线标记的存活时间。
const (
	bindingTTL  = 10 * time.Minute
	presenceTTL = 5 * time.Minute
)

// ErrNotBound 表示消息涉及的会话未在服务端登记。
var ErrNotBound = errors.New("signal: session not bound")

// Conn 是发送终点的最小接口，便于用假实现测试。
type Conn interface {
	Send(msgType int, body any) error
	Close() error
}

// Hub 维护在线设备与会话绑定。所有对外方法都要求设备已在线。
type Hub struct {
	mu         sync.RWMutex
	devices    map[string]Conn
	sessions   map[string]*Binding
	now        func() time.Time
	bus        Bus
	instanceID string
	// connGen 每次 Register 递增；presence 值写成 "instance#gen"，
	// 旧连接关闭时不会误删新连接的在线标记（审查项 S5）。
	connGen uint64
}

// SetBus 注入跨实例总线（多实例部署时必须设置）。
func (h *Hub) SetBus(b Bus) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.bus = b
}

// Binding 是一次建连请求的服务端记录。
// 关键：候选交换与授权结果只允许在已绑定的两端之间转发，防止任意设备注入候选。
type Binding struct {
	SessionID string
	From      string // 主控 deviceID
	To        string // 被控 deviceID
	CreatedAt time.Time
}

// NewHub 创建 Hub。
func NewHub() *Hub {
	return &Hub{devices: map[string]Conn{}, sessions: map[string]*Binding{}, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock 仅测试使用。
func (h *Hub) SetClock(f func() time.Time) { h.now = f }

// Register 注册在线设备。同一设备重复注册时关闭旧连接并返回被替换的旧连接。
func (h *Hub) Register(deviceID string, c Conn) Conn {
	h.mu.Lock()
	old := h.devices[deviceID]
	h.devices[deviceID] = c
	h.connGen++
	token := h.presenceToken()
	bus := h.bus
	h.mu.Unlock()
	if bus != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		_ = bus.SetOnline(ctx, deviceID, token, presenceTTL)
	}
	return old
}

// presenceToken 返回本实例+本次连接的唯一标记，必须在 h.mu 持有下调用。
func (h *Hub) presenceToken() string {
	return h.instanceID + "#" + itoaU(h.connGen)
}

func itoaU(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// SetInstanceID 设置本实例标识，用于共享在线表。
func (h *Hub) SetInstanceID(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.instanceID = id
}

// Unregister 下线设备，仅当当前连接仍是 self 时才删除
// （避免旧连接关闭时把新连接踢掉——审查项 S12-4）。
func (h *Hub) Unregister(deviceID string, self Conn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	cur, ok := h.devices[deviceID]
	if !ok || cur != self {
		return false
	}
	delete(h.devices, deviceID)
	token := h.presenceToken()
	bus := h.bus
	var sids []string
	for sid, b := range h.sessions {
		if b.From == deviceID || b.To == deviceID {
			delete(h.sessions, sid)
			sids = append(sids, sid)
		}
	}
	// 同步清理，带超时：异步 goroutine 在 Redis 故障时会堆积，
	// 且可能与新连接竞态误删在线标记（审查项 S5）。
	if bus != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		_ = bus.ClearOnline(ctx, deviceID, token)
		for _, sid := range sids {
			_ = bus.DeleteBinding(ctx, sid)
		}
	}
	return true
}

// Touch 续租在线标记。只有当前连接可以续租；心跳丢失后由 TTL 自动过期。
func (h *Hub) Touch(deviceID string, self Conn) bool {
	h.mu.RLock()
	cur, ok := h.devices[deviceID]
	bus := h.bus
	token := h.presenceToken()
	h.mu.RUnlock()
	if !ok || cur != self {
		return false
	}
	if bus != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		_ = bus.SetOnline(ctx, deviceID, token, presenceTTL)
	}
	return true
}

// Online 判断设备是否在线。
func (h *Hub) Online(deviceID string) bool {
	h.mu.RLock()
	_, local := h.devices[deviceID]
	bus := h.bus
	h.mu.RUnlock()
	if local {
		return true
	}
	if bus != nil {
		// 跨实例：查共享在线表
		if rb, ok := bus.(*RedisBus); ok {
			online, err := rb.IsOnline(context.Background(), deviceID)
			return err == nil && online
		}
	}
	return false
}

// Bind 登记一次会话绑定。请求方必须在线，且被控方不能被自己绑定。
func (h *Hub) Bind(sessionID, from, to string) (*Binding, error) {
	if sessionID == "" || from == "" || to == "" || from == to {
		return nil, ErrNotBound
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.devices[from]; !ok {
		return nil, ErrNotBound
	}
	b := &Binding{SessionID: sessionID, From: from, To: to, CreatedAt: h.now()}
	h.sessions[sessionID] = b
	bus := h.bus
	if bus != nil {
		_ = bus.SaveBinding(context.Background(), b, bindingTTL)
	}
	return b, nil
}

// Binding 查询绑定。
func (h *Hub) Binding(sessionID string) (*Binding, bool) {
	h.mu.RLock()
	b, ok := h.sessions[sessionID]
	bus := h.bus
	h.mu.RUnlock()
	if ok {
		cp := *b
		return &cp, true
	}
	if bus != nil {
		if rb, err := bus.LoadBinding(context.Background(), sessionID); err == nil {
			return rb, true
		}
	}
	return nil, false
}

// NotifyConsentRequired 通知被控端有连接请求。
// deliver 先尝试本地连接，再回退到总线（跨实例）。
func (h *Hub) deliver(deviceID string, msgType int, body any) error {
	h.mu.RLock()
	target := h.devices[deviceID]
	bus := h.bus
	h.mu.RUnlock()

	if target != nil {
		return target.Send(msgType, body)
	}
	if bus == nil {
		return ErrNotBound
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return bus.Publish(context.Background(), deviceID, Envelope{Type: msgType, Body: raw})
}

// DeliverLocal 由总线回调：把跨实例消息投递给本机连接。
func (h *Hub) DeliverLocal(deviceID string, msg Envelope) error {
	h.mu.RLock()
	target := h.devices[deviceID]
	h.mu.RUnlock()
	if target == nil {
		return nil
	}
	var body any
	_ = json.Unmarshal(msg.Body, &body)
	return target.Send(msg.Type, body)
}

// BindFromServer 由服务端（网关）建立绑定。
//
// 走 HTTP 建会话时，主控并没有信令长连接，无法走 Bind 的「双方必须在线」校验；
// 但 HTTP 层已经验证过账号与设备身份，因此这里允许直接登记。
func (h *Hub) BindFromServer(sessionID, from, to string) (*Binding, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	b := &Binding{SessionID: sessionID, From: from, To: to, CreatedAt: h.now()}
	h.sessions[sessionID] = b
	bus := h.bus
	if bus != nil {
		_ = bus.SaveBinding(context.Background(), b, bindingTTL)
	}
	return b, nil
}

func (h *Hub) NotifyConsentRequired(sessionID, from, to string) error {
	b, ok := h.Binding(sessionID)
	if !ok {
		return ErrNotBound
	}
	return h.deliver(b.To, TypeConsentRequired, map[string]any{"session_id": sessionID, "from": from})
}

// ForwardConsent 把被控端的授权结果转发给主控端。
// 只有绑定里的 To 才能代表该会话做出授权决策。
func (h *Hub) ForwardConsent(sessionID, fromDevice string, accepted bool) error {
	b, ok := h.Binding(sessionID)
	if !ok || fromDevice != b.To {
		return ErrNotBound // 非被控端无权授权
	}
	if accepted {
		return h.deliver(b.From, TypeConsentGranted, map[string]any{"session_id": sessionID})
	}
	return h.deliver(b.From, TypeDisconnect, map[string]any{"session_id": sessionID, "reason": "rejected"})
}

// ForwardCandidates 交换 ICE 候选。只在绑定两端之间转发，且不泄露给第三方。
func (h *Hub) ForwardCandidates(sessionID, fromDevice string, candidates []any) error {
	b, ok := h.Binding(sessionID)
	if !ok {
		return ErrNotBound
	}
	var to string
	switch fromDevice {
	case b.From:
		to = b.To
	case b.To:
		to = b.From
	default:
		return ErrNotBound
	}
	return h.deliver(to, TypeCandidates, map[string]any{"session_id": sessionID, "to": to, "candidates": candidates})
}

// Disconnect 通知对端断开。
func (h *Hub) Disconnect(sessionID, fromDevice, reason string) error {
	b, ok := h.Binding(sessionID)
	if !ok {
		return ErrNotBound
	}
	var to string
	if fromDevice == b.From {
		to = b.To
	} else if fromDevice == b.To {
		to = b.From
	} else {
		return ErrNotBound
	}
	h.mu.Lock()
	delete(h.sessions, sessionID)
	bus := h.bus
	h.mu.Unlock()
	if bus != nil {
		_ = bus.DeleteBinding(context.Background(), sessionID)
	}
	return h.deliver(to, TypeDisconnect, map[string]any{"session_id": sessionID, "reason": reason})
}

// NotifyClosed 向会话双方投递 DISCONNECT 并清理绑定。
// 与 Disconnect 不同：它不要求 from 是会话方已在线，用于状态机关闭后的传播。
func (h *Hub) NotifyClosed(sessionID, from, to, reason string) error {
	h.mu.Lock()
	delete(h.sessions, sessionID)
	bus := h.bus
	h.mu.Unlock()
	body := map[string]any{"session_id": sessionID, "reason": reason}
	var firstErr error
	for _, dev := range []string{from, to} {
		if dev == "" {
			continue
		}
		if err := h.deliver(dev, TypeDisconnect, body); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if bus != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		if err := bus.DeleteBinding(ctx, sessionID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Counts 返回在线设备与会话数，供健康检查使用。
func (h *Hub) Counts() (devices, sessions int) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.devices), len(h.sessions)
}
