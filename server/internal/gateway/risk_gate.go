package gateway

import (
	"context"
	"errors"
	"time"

	"github.com/easyremote/server/internal/store"
)

// riskBlocked 是连接前的风控闸（ADM-API-06c）：账号 / 目标设备码 / 来源 IP 任一命中黑名单即返回命中主体。
// 返回 ("", nil) 表示放行；返回错误表示风控存储不可用——此时按失败处理，不静默放行。
func (a *App) riskBlocked(ctx context.Context, accountID, deviceCode, ip string) (string, error) {
	now := time.Now().UTC()
	for _, probe := range []struct{ kind, value string }{
		{"account", accountID},
		{"device", deviceCode},
	} {
		if probe.value == "" {
			continue
		}
		if _, err := a.Store.FindActiveBlacklist(ctx, probe.kind, probe.value, now); err == nil {
			return probe.kind + ":" + probe.value, nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return "", err
		}
	}
	if ip != "" {
		if _, err := a.Store.MatchIPBlacklist(ctx, ip, now); err == nil {
			return "ip:" + ip, nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return "", err
		}
	}
	return "", nil
}

// 复用 handler.go 里已有的 clientIP；这里不再重复定义。
