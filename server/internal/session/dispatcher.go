package session

import (
	"context"
	"fmt"

	"github.com/easyremote/server/internal/store"
)

// EventHandler 消费一条 outbox 事件。实现必须按 event ID 幂等。
type EventHandler func(ctx context.Context, ev *store.SessionEvent) error

// Dispatcher 从 outbox 读取待投递事件；失败保留并累计 attempts。
type Dispatcher struct {
	store   store.Store
	handler EventHandler
}

func NewDispatcher(s store.Store, h EventHandler) *Dispatcher {
	return &Dispatcher{store: s, handler: h}
}

// DispatchOnce 处理最多 limit 条待投递事件，返回成功/失败计数。
func (d *Dispatcher) DispatchOnce(ctx context.Context, limit int) (delivered, failed int, err error) {
	events, err := d.store.ListSessionEvents(ctx, false, limit)
	if err != nil {
		return 0, 0, err
	}
	for _, ev := range events {
		if herr := d.handler(ctx, ev); herr != nil {
			failed++
			_ = d.store.MarkSessionEventFailed(ctx, ev.ID, herr.Error())
			continue
		}
		if merr := d.store.MarkSessionEventDelivered(ctx, ev.ID); merr != nil {
			return delivered, failed, fmt.Errorf("mark delivered %s: %w", ev.ID, merr)
		}
		delivered++
	}
	return delivered, failed, nil
}
