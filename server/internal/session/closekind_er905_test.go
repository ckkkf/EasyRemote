package session

import "testing"

// ER-905：新增的 reason 必须落到正确 CloseKind，不能全部掉进 unknown。
// unknown 在 abnormalKinds 里 = 异常中断，会让后台异常率失真。
func TestClassifyCloseCoversEr905Reasons(t *testing.T) {
	cases := []struct {
		reason string
		want   CloseKind
	}{
		{"user_rejected", CloseUserHangup},
		{"controller_closed", CloseUserHangup},
		{"host_closed", CloseUserHangup},
		{"closed_by_self", CloseUserHangup},
		// 被控端本地点断开的真实 reason：发起方是用户本人，必须算 user_hangup，
		// 不能被 contains("disconnect") 判成 peer_offline（异常）。
		{"local_disconnect", CloseUserHangup},
		{"peer_closed", ClosePeerOffline},
		{"auth_window_exceeded", CloseTimeout},
		{"consent_timeout", CloseTimeout},
		{"request_expired", CloseTimeout},
	}
	for _, c := range cases {
		if got := ClassifyClose(c.reason); got != c.want {
			t.Fatalf("ClassifyClose(%q) = %s, want %s", c.reason, got, c.want)
		}
	}

	// 不变量：用户主动结束不算异常；超时/对端离开算异常。
	if abnormalKinds[ClassifyClose("user_rejected")] {
		t.Fatal("用户拒绝不应计入异常中断")
	}
	if abnormalKinds[ClassifyClose("controller_closed")] {
		t.Fatal("用户主动断开不应计入异常中断")
	}
	if abnormalKinds[ClassifyClose("local_disconnect")] {
		t.Fatal("被控端本地断开是用户主动行为，不应计入异常中断")
	}
	if !abnormalKinds[ClassifyClose("peer_closed")] {
		t.Fatal("对端离开应计入异常中断")
	}
}
