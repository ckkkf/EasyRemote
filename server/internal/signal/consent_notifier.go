package signal

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ConsentNotifier 是网关侧的生产通知器：通过共享 Bus 把授权请求投递到
// 目标设备所在的信令实例，不再依赖进程内全局 Hub。
type ConsentNotifier struct {
	Bus     Bus
	Timeout time.Duration
	TTL     time.Duration
}

// NextTicketEpoch 为设备分配新的票据代号；Bus 不支持时返回 0（单机模式）。
func (n ConsentNotifier) NextTicketEpoch(ctx context.Context, sessionID, deviceID string) (int64, error) {
	if e, ok := n.Bus.(interface {
		NextTicketEpoch(context.Context, string, string) (int64, error)
	}); ok {
		return e.NextTicketEpoch(ctx, sessionID, deviceID)
	}
	return 0, nil
}

// NotifySessionClosed 向会话双方投递 DISCONNECT，并删除共享绑定。
func (n ConsentNotifier) NotifySessionClosed(sessionID, from, to, reason string) error {
	if n.Bus == nil {
		return errors.New("signal: consent notifier has no bus")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	body, err := json.Marshal(map[string]string{"session_id": sessionID, "reason": reason})
	if err != nil {
		return err
	}
	var firstErr error
	for _, dev := range []string{from, to} {
		if dev == "" {
			continue
		}
		if err := n.Bus.Publish(ctx, dev, Envelope{Type: TypeDisconnect, Body: body}); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := n.Bus.DeleteBinding(ctx, sessionID); err != nil && firstErr == nil {
		firstErr = err
	}
	// 旧票据必须立刻失效；中继注册时检查共享撤销键。
	if revoker, ok := n.Bus.(interface {
		RevokeSession(context.Context, string, time.Duration) error
	}); ok {
		if err := revoker.RevokeSession(ctx, sessionID, 2*time.Hour); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// NotifyConsentRequired 写入跨实例绑定并投递 CONSENT_REQUIRED。
func (n ConsentNotifier) NotifyConsentRequired(sessionID, from, to string) error {
	if n.Bus == nil {
		return errors.New("signal: consent notifier has no bus")
	}
	timeout := n.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	ttl := n.TTL
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// 绑定必须跨实例可见：被控端在实例 B 上回传授权结果时需要读到它。
	if err := n.Bus.SaveBinding(ctx, &Binding{
		SessionID: sessionID,
		From:      from,
		To:        to,
		CreatedAt: time.Now().UTC(),
	}, ttl); err != nil {
		return err
	}
	body, err := json.Marshal(map[string]string{"session_id": sessionID, "from": from})
	if err != nil {
		return err
	}
	return n.Bus.Publish(ctx, to, Envelope{Type: TypeConsentRequired, Body: body})
}
