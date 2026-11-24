package gateway

import (
	"context"
	"errors"

	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/store"
)

// recordSessionError 记录会话级错误事件（ADM-API-03h）。
//
// 这是"尽力而为"的遥测：写入失败不能覆盖原始错误响应（否则用户看到的是
// 遥测失败而不是真实原因）。因此这里的错误被显式丢弃，并在此注释说明理由。
func (a *App) recordSessionError(ctx context.Context, sessionID, source string, err error) {
	if sessionID == "" || err == nil {
		return
	}
	code := errcode.SrvInternal
	detail := err.Error()
	var ec *errcode.Error
	if errors.As(err, &ec) {
		code = ec.Code
		if ec.Detail != "" {
			detail = ec.Detail
		}
	}
	_ = a.Store.RecordSessionError(ctx, &store.SessionError{
		SessionID: sessionID,
		Code:      int(code),
		Name:      errcode.Name(code),
		Detail:    detail,
		Source:    source,
	})
}
