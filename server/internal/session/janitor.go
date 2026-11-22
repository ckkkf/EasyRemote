package session

import (
	"context"
	"time"
)

// Presence 是权威在线判定（signal.Presence 的窄接口）。
type Presence interface {
	Online(ctx context.Context, deviceID string) (bool, error)
}

// Closer 把 janitor 的强制关闭传播给双方。
type Closer interface {
	NotifySessionClosed(sessionID, from, to, reason string) error
}

// Janitor 清理“双方已离线但状态仍 active”的僵尸会话。
//
// 在线状态来自信令租约（TTL），不新增会话心跳：进程崩溃后租约过期，
// 下一次 Sweep 会把 active 关闭并传播。presence 查询失败时跳过而不是关闭，
// 避免共享 Bus 抖动导致批量误杀。
type Janitor struct {
	Sessions *Service
	Presence Presence
	Closer   Closer
	Interval time.Duration
}

// Sweep 关闭所有参与者离线的 active 会话，返回关闭数量。
func (j *Janitor) Sweep(ctx context.Context) (int, error) {
	if j.Sessions == nil || j.Presence == nil {
		return 0, nil
	}
	list, err := j.Sessions.List(ctx, StateActive)
	if err != nil {
		return 0, err
	}
	closed := 0
	for _, sess := range list {
		ctrlOnline, err := j.Presence.Online(ctx, sess.ControllerID)
		if err != nil {
			continue
		}
		targetOnline, err := j.Presence.Online(ctx, sess.TargetID)
		if err != nil {
			continue
		}
		if ctrlOnline && targetOnline {
			continue
		}
		if _, err := j.Sessions.Apply(ctx, Command{
			Kind:      CommandClose,
			SessionID: sess.ID,
			Reason:    "peer_offline",
		}); err != nil {
			continue
		}
		if j.Closer != nil {
			_ = j.Closer.NotifySessionClosed(sess.ID, sess.ControllerID, sess.TargetID, "peer_offline")
		}
		closed++
	}
	return closed, nil
}

// Run 周期执行 Sweep，直到 ctx 取消。
func (j *Janitor) Run(ctx context.Context) {
	interval := j.Interval
	if interval <= 0 {
		interval = 10 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_, _ = j.Sweep(ctx)
		}
	}
}
