// Package ratelimit 提供跨实例共享的限流计数器。
//
// 为什么必须共享：限流若放在进程内存，多实例部署时把请求打散到不同实例
// 即可绕过（验证码爆破、短信轰炸、登录爆破都属此类）。
package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Result 是一次限流判定的结果。
type Result struct {
	Allowed    bool
	Remaining  int
	RetryAfter time.Duration
}

// Limiter 是限流接口。实现必须原子：并发下计数不能丢。
type Limiter interface {
	// Allow 在窗口内对 key 计数一次；返回是否放行。
	Allow(ctx context.Context, key string, limit int, window time.Duration) (Result, error)
	// Reset 清空某个 key（例如登录成功后清零失败计数）。
	Reset(ctx context.Context, key string) error
	Ping(ctx context.Context) error
}

// Memory 是单实例实现，仅用于测试与本地开发。
type Memory struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	now     func() time.Time
}

type bucket struct {
	count   int
	resetAt time.Time
}

// NewMemory 创建内存限流器。
func NewMemory() *Memory {
	return &Memory{buckets: map[string]*bucket{}, now: func() time.Time { return time.Now().UTC() }}
}

var _ Limiter = (*Memory)(nil)

// SetClock 仅测试使用。
func (m *Memory) SetClock(f func() time.Time) { m.now = f }

func (m *Memory) Allow(_ context.Context, key string, limit int, window time.Duration) (Result, error) {
	if window <= 0 {
		return Result{}, fmt.Errorf("ratelimit: window must be positive")
	}
	if limit <= 0 {
		return Result{}, fmt.Errorf("ratelimit: limit must be positive")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	b, ok := m.buckets[key]
	if !ok || now.After(b.resetAt) {
		b = &bucket{count: 0, resetAt: now.Add(window)}
		m.buckets[key] = b
	}
	if b.count >= limit {
		return Result{Allowed: false, Remaining: 0, RetryAfter: b.resetAt.Sub(now)}, nil
	}
	b.count++
	return Result{Allowed: true, Remaining: limit - b.count}, nil
}

func (m *Memory) Reset(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.buckets, key)
	return nil
}

func (m *Memory) Ping(context.Context) error { return nil }
