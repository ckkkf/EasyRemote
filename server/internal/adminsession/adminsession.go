package adminsession

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

// Session 是一次后台登录会话。
type Session struct {
	Token     string
	AdminID   string
	Username  string
	Role      string
	CreatedAt time.Time
	ExpiresAt time.Time
	Revoked   bool
}

// ErrSessionNotFound 表示令牌不存在或已过期。
var ErrSessionNotFound = errors.New("adminsession: not found")

// Store 是会话存储抽象。内存实现用于单实例/测试，Redis 实现用于多实例部署。
type Store interface {
	Put(ctx context.Context, s *Session) error
	Get(ctx context.Context, token string) (*Session, error)
	Revoke(ctx context.Context, token string) error
	RevokeAllForAdmin(ctx context.Context, adminID string) error
	Ping(ctx context.Context) error
}

// Service 管理后台会话。令牌由服务端生成，角色只来自存储。
type Service struct {
	store Store
	ttl   time.Duration
	now   func() time.Time
}

// New 创建会话服务。store 为 nil 时退化为进程内存储（单实例）。
func New(store Store, ttl time.Duration) *Service {
	if ttl <= 0 {
		ttl = 8 * time.Hour
	}
	if store == nil {
		store = NewMemoryStore()
	}
	return &Service{store: store, ttl: ttl, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock 仅测试使用。
func (s *Service) SetClock(f func() time.Time) { s.now = f }

func newToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// Issue 签发新会话令牌。
func (s *Service) Issue(ctx context.Context, adminID, username, role string) (string, *Session, error) {
	token, err := newToken()
	if err != nil {
		return "", nil, err
	}
	now := s.now()
	sess := &Session{
		Token:     token,
		AdminID:   adminID,
		Username:  username,
		Role:      role,
		CreatedAt: now,
		ExpiresAt: now.Add(s.ttl),
	}
	if err := s.store.Put(ctx, sess); err != nil {
		return "", nil, err
	}
	return token, sess, nil
}

// Verify 校验令牌：不存在、过期、已撤销一律拒绝。
func (s *Service) Verify(ctx context.Context, token string) (*Session, bool) {
	if token == "" {
		return nil, false
	}
	sess, err := s.store.Get(ctx, token)
	if err != nil || sess.Revoked {
		return nil, false
	}
	if s.now().After(sess.ExpiresAt) {
		return nil, false
	}
	if subtle.ConstantTimeCompare([]byte(sess.Token), []byte(token)) != 1 {
		return nil, false
	}
	return sess, true
}

// Revoke 撤销单个令牌（登出）。
func (s *Service) Revoke(ctx context.Context, token string) error {
	return s.store.Revoke(ctx, token)
}

// RevokeAllForAdmin 撤销某后台账号的全部会话。
func (s *Service) RevokeAllForAdmin(ctx context.Context, adminID string) error {
	return s.store.RevokeAllForAdmin(ctx, adminID)
}

// MemoryStore 是进程内实现。
type MemoryStore struct {
	mu      sync.RWMutex
	byToken map[string]*Session
}

// NewMemoryStore 创建内存存储。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byToken: map[string]*Session{}}
}

var _ Store = (*MemoryStore)(nil)

func (m *MemoryStore) Put(_ context.Context, s *Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *s
	m.byToken[s.Token] = &cp
	return nil
}

func (m *MemoryStore) Get(_ context.Context, token string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.byToken[token]
	if !ok {
		return nil, ErrSessionNotFound
	}
	cp := *s
	return &cp, nil
}

func (m *MemoryStore) Revoke(_ context.Context, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.byToken[token]; ok {
		s.Revoked = true
	}
	return nil
}

func (m *MemoryStore) RevokeAllForAdmin(_ context.Context, adminID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.byToken {
		if s.AdminID == adminID {
			s.Revoked = true
		}
	}
	return nil
}

func (m *MemoryStore) Ping(context.Context) error { return nil }
