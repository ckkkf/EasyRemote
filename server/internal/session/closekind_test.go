package session

import "testing"

// ADM-API-02f1：结束原因归一化，统计口径只由这里决定。
func TestClassifyCloseKinds(t *testing.T) {
	cases := []struct {
		reason string
		want   CloseKind
	}{
		{"user hangup", CloseUserHangup},
		{"User Hangup", CloseUserHangup},
		{"user closed", CloseUserHangup},
		{"admin: 安全应急", CloseAdminKill},
		{"admin force-offline: 用户报障", CloseAdminKill},
		{"auth timeout", CloseTimeout},
		{"consent timeout", CloseTimeout},
		{"network changed", CloseNetworkLost},
		{"connection lost", CloseNetworkLost},
		{"peer offline", ClosePeerOffline},
		{"peer closed", ClosePeerOffline},
		{"", CloseUnknown},
		{"???", CloseUnknown},
	}
	for _, c := range cases {
		if got := ClassifyClose(c.reason); got != c.want {
			t.Fatalf("ClassifyClose(%q)=%s want %s", c.reason, got, c.want)
		}
	}
}

func TestIsAbnormalExcludesUserAndAdmin(t *testing.T) {
	if IsAbnormal("user hangup") {
		t.Fatal("用户主动挂断不算异常")
	}
	if IsAbnormal("admin: 违规") {
		t.Fatal("后台人工中断不算异常")
	}
	for _, r := range []string{"auth timeout", "network changed", "peer offline", ""} {
		if !IsAbnormal(r) {
			t.Fatalf("%q 应算异常中断", r)
		}
	}
}
