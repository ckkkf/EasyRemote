package gateway

import (
	"time"

	"github.com/easyremote/server/internal/store"
)

// storeAssistCode 构造一条协助码记录（测试专用，避免在用例里重复写结构体字段）。
func storeAssistCode(code, accountID, targetID string, expiresAt time.Time) store.AssistCode {
	return store.AssistCode{
		Code:      code,
		AccountID: accountID,
		TargetID:  targetID,
		CreatedAt: expiresAt.Add(-10 * time.Minute),
		ExpiresAt: expiresAt,
	}
}
