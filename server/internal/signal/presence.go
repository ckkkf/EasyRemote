package signal

import "context"

// Presence 是网关侧的在线判定：本地 Hub 优先，跨实例回落到共享 Bus。
//
// 生产网关必须注入它，否则“目标离线”无法在建会话前判定，只能返回假 requested。
type Presence struct {
	Hub *Hub
	Bus Bus
}

func (p Presence) Online(ctx context.Context, deviceID string) (bool, error) {
	if p.Hub != nil && p.Hub.Online(deviceID) {
		return true, nil
	}
	if p.Bus != nil {
		return p.Bus.IsOnline(ctx, deviceID)
	}
	return false, nil
}
