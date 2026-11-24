package session

import "strings"

// CloseKind 是会话结束原因的归一化分类（ADM-API-02f1）。
//
// 为什么需要它：session.Reason 是自由文本（客户端传的 "user hangup"、后台拼的
// "admin: 安全应急"、超时路径写的 "auth timeout"），直接统计会把"用户正常挂断"
// 和"网络掉线"混在一起，异常中断率就失去意义。
//
// 分类只做字符串归一，不改变 Reason 本身：原始文本继续保留在审计与详情页里，
// 统计口径由此处唯一决定。
type CloseKind string

const (
	CloseUserHangup  CloseKind = "user_hangup"
	CloseAdminKill   CloseKind = "admin_kill"
	CloseTimeout     CloseKind = "timeout"
	ClosePeerOffline CloseKind = "peer_offline"
	CloseNetworkLost CloseKind = "network_lost"
	CloseUnknown     CloseKind = "unknown"
)

// abnormalKinds 定义"异常中断"：用户主动挂断与后台人工中断都不算异常。
var abnormalKinds = map[CloseKind]bool{
	CloseTimeout:     true,
	ClosePeerOffline: true,
	CloseNetworkLost: true,
	CloseUnknown:     true,
}

// ClassifyClose 把自由文本的结束原因归一成 CloseKind。
// 匹配顺序从具体到宽泛，避免 "admin force-offline" 被误判成 offline。
func ClassifyClose(reason string) CloseKind {
	r := strings.ToLower(strings.TrimSpace(reason))
	switch {
	case r == "":
		return CloseUnknown
	case strings.HasPrefix(r, "admin"):
		return CloseAdminKill
	// ER-905 的 reason 必须显式映射：它们是带下划线的机器字符串，
	// 走下面的 contains 规则会全部落到 unknown，而 unknown 属于「异常中断」，
	// 会把「用户拒绝」「用户主动断开」误算成故障，污染后台异常率。
	case r == "user_rejected":
		return CloseUserHangup
	case r == "controller_closed", r == "host_closed", r == "closed_by_self":
		return CloseUserHangup
	// local_disconnect 是被控端「本地点断开」写的 reason。
	// 不显式映射的话会被下面的 contains("disconnect") 判成 peer_offline（异常），
	// 而发起方其实是用户本人——必须算 user_hangup。
	// 注意与服务端统计口径无关的客户端语义：主控看到同一个 reason 会显示
	// 「对方已结束远程控制」，那是观察点不同，不是分类冲突。
	case r == "local_disconnect":
		return CloseUserHangup
	case r == "peer_closed":
		return ClosePeerOffline
	case r == "auth_window_exceeded":
		return CloseTimeout
	case strings.Contains(r, "timeout"), strings.Contains(r, "timed out"), strings.Contains(r, "expired"):
		return CloseTimeout
	case strings.Contains(r, "network"), strings.Contains(r, "connection lost"), strings.Contains(r, "udp"):
		return CloseNetworkLost
	case strings.Contains(r, "offline"), strings.Contains(r, "peer closed"), strings.Contains(r, "disconnect"):
		return ClosePeerOffline
	case strings.Contains(r, "hangup"), strings.Contains(r, "hang up"), strings.Contains(r, "user close"), strings.Contains(r, "user closed"):
		return CloseUserHangup
	default:
		return CloseUnknown
	}
}

// IsAbnormal 判断某个结束原因是否算异常中断。
func IsAbnormal(reason string) bool {
	return abnormalKinds[ClassifyClose(reason)]
}
