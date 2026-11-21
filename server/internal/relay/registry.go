package relay

import (
	"context"
	"sync"
	"time"
)

// Registry 是多节点中继的目录服务：
//   - node 目录：nodeID → 该节点的 UDP 地址
//   - route 目录：(sessionID, deviceID) → 该设备注册在哪个节点
//
// 单节点部署可传 nil；多节点部署必须用 Redis 实现，否则两端注册到不同
// 节点时数据无法互达（这是与限流/会话同类的多实例隐患）。
type Registry interface {
	RegisterNode(ctx context.Context, nodeID, addr string, ttl time.Duration) error
	LookupNode(ctx context.Context, nodeID string) (string, error)
	SetRoute(ctx context.Context, sessionID, deviceID, nodeID string, ttl time.Duration) error
	LookupRoute(ctx context.Context, sessionID, deviceID string) (string, error)
	ClearRoute(ctx context.Context, sessionID, deviceID string) error
	Ping(ctx context.Context) error
}

// MemoryRegistry 是单节点/测试实现。
type MemoryRegistry struct {
	mu     sync.RWMutex
	nodes  map[string]nodeEntry
	routes map[string]routeEntry
	now    func() time.Time
}

type nodeEntry struct {
	addr    string
	expires time.Time
}

type routeEntry struct {
	nodeID  string
	expires time.Time
}

// NewMemoryRegistry 创建内存目录。
func NewMemoryRegistry() *MemoryRegistry {
	return &MemoryRegistry{
		nodes:  map[string]nodeEntry{},
		routes: map[string]routeEntry{},
		now:    func() time.Time { return time.Now().UTC() },
	}
}

var _ Registry = (*MemoryRegistry)(nil)

// SetClock 仅测试使用。
func (m *MemoryRegistry) SetClock(f func() time.Time) { m.now = f }

func routeKey(sessionID, deviceID string) string { return sessionID + "|" + deviceID }

func (m *MemoryRegistry) RegisterNode(_ context.Context, nodeID, addr string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodes[nodeID] = nodeEntry{addr: addr, expires: m.now().Add(ttl)}
	return nil
}

func (m *MemoryRegistry) LookupNode(_ context.Context, nodeID string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.nodes[nodeID]
	if !ok || m.now().After(e.expires) {
		return "", ErrShort
	}
	return e.addr, nil
}

func (m *MemoryRegistry) SetRoute(_ context.Context, sessionID, deviceID, nodeID string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.routes[routeKey(sessionID, deviceID)] = routeEntry{nodeID: nodeID, expires: m.now().Add(ttl)}
	return nil
}

func (m *MemoryRegistry) LookupRoute(_ context.Context, sessionID, deviceID string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.routes[routeKey(sessionID, deviceID)]
	if !ok || m.now().After(e.expires) {
		return "", ErrShort
	}
	return e.nodeID, nil
}

func (m *MemoryRegistry) ClearRoute(_ context.Context, sessionID, deviceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.routes, routeKey(sessionID, deviceID))
	return nil
}

func (m *MemoryRegistry) Ping(context.Context) error { return nil }
