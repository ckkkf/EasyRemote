package relay

import (
	"context"

	"github.com/easyremote/server/internal/relaymgr"
)

// TicketEpochCurrent 判断票据代号是否仍是该设备最新代号。
// epoch=0（单机模式）或注册表不支持时放行；后端错误按失败拒绝（fail-closed）。
func TicketEpochCurrent(ctx context.Context, reg Registry, tk *relaymgr.Ticket) bool {
	if tk == nil || tk.Epoch == 0 {
		return true
	}
	reader, ok := reg.(interface {
		TicketEpoch(context.Context, string, string) (int64, error)
	})
	if !ok {
		return true
	}
	cur, err := reader.TicketEpoch(ctx, tk.SessionID, tk.Self)
	if err != nil {
		return false
	}
	return cur == tk.Epoch
}

// IsSessionRevoked 判断会话是否已被关闭（共享 Redis 撤销键）。
// 注册票据前必须调用；无 Redis（开发单机）时返回 false。
func IsSessionRevoked(ctx context.Context, reg Registry, sessionID string) bool {
	checker, ok := reg.(interface {
		IsRevoked(context.Context, string) (bool, error)
	})
	if !ok {
		return false
	}
	revoked, err := checker.IsRevoked(ctx, sessionID)
	return err == nil && revoked
}
