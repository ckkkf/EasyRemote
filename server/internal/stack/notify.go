package stack

import (
	"github.com/easyremote/server/internal/signal"
	"log"
)

// devHub 是一体化部署时的全局信令 hub。
//
// 同进程部署：网关建会话后直接通知 hub；
// 多进程部署应换成 Redis Pub/Sub（signal.Bus 已有该能力）。
var devHub *signal.Hub

// setDevHub 由装配层调用，注册当前进程的 hub。
func SetDevHub(h *signal.Hub) { devHub = h }

// gatewayNotifier 与 gateway.ConsentNotifier 结构一致（避免包循环）。
type gatewayNotifier interface {
	NotifyConsentRequired(sessionID, from, to string) error
}

type hubNotifier struct {
	hub *signal.Hub
}

func (h hubNotifier) NotifyConsentRequired(sessionID, from, to string) error {
	// 先登记绑定：HTTP 建会话的主控不在信令长连接上
	_, _ = h.hub.BindFromServer(sessionID, from, to)
	err := h.hub.NotifyConsentRequired(sessionID, from, to)
	log.Printf("notify: session=%s from=%s to=%s err=%v", sessionID, from, to, err)
	return err
}

// notifierFromEnv 返回当前进程内可用的通知器；未注册 hub 时返回 nil。
func NotifierFromEnv() gatewayNotifier {
	if devHub == nil {
		return nil
	}
	return hubNotifier{hub: devHub}
}
