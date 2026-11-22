package capability

import "testing"

// TestIncompleteCapabilityIsDisabled 对应 ER-000 验收断言：
// 所有非 stable 能力在服务端必须报告不可用，且 ER_CAPABILITY_ENABLE 不能把它们打开。
func TestIncompleteCapabilityIsDisabled(t *testing.T) {
	t.Setenv("ER_CAPABILITY_ENABLE", "remote_control,remote_host,privacy_screen,file_transfer")
	t.Setenv("ER_CAPABILITY_DISABLE", "")
	pub := Public()
	if len(pub) != len(Specs()) {
		t.Fatalf("Public() 覆盖 %d 条能力，台账有 %d 条", len(pub), len(Specs()))
	}
	for _, s := range Specs() {
		if s.Maturity == MaturityStable {
			continue
		}
		if pub[s.Key] {
			t.Errorf("能力 %s（%s）被报告为可用；未达 stable 必须 fail-closed", s.Key, s.Maturity)
		}
		if Enabled(s.Key) {
			t.Errorf("Enabled(%q) = true；非 stable 能力不得被服务端承诺", s.Key)
		}
	}
}

// TestDisableListOnlySubtracts：ER_CAPABILITY_DISABLE 只能关，ER_CAPABILITY_ENABLE 不能开。
func TestDisableListOnlySubtracts(t *testing.T) {
	t.Setenv("ER_CAPABILITY_ENABLE", "remote_control")
	if Enabled("remote_control") {
		t.Fatal("未成熟能力被 ER_CAPABILITY_ENABLE 打开了")
	}
	t.Setenv("ER_CAPABILITY_DISABLE", "remote_control")
	if Enabled("remote_control") {
		t.Fatal("ER_CAPABILITY_DISABLE 之后仍报告可用")
	}
	if Public()["remote_control"] {
		t.Fatal("Public() 忽略了禁用列表")
	}
}

// TestLedgerIsComplete：台账每条都要有 owner 与 reason，避免出现「无主能力」。
func TestLedgerIsComplete(t *testing.T) {
	if len(Specs()) == 0 {
		t.Fatal("能力台账为空")
	}
	for _, s := range Specs() {
		if s.Key == "" || s.Title == "" || s.Owner == "" || s.Reason == "" {
			t.Errorf("能力台账字段不完整: %+v", s)
		}
		switch s.Maturity {
		case MaturityDisabled, MaturityExperimental, MaturityBeta, MaturityStable:
		default:
			t.Errorf("能力 %s 成熟度非法: %q", s.Key, s.Maturity)
		}
	}
}

func TestUnknownCapabilityFailsClosed(t *testing.T) {
	if Enabled("not_a_capability") {
		t.Fatal("未登记能力被报告为可用")
	}
	if _, ok := Lookup("not_a_capability"); ok {
		t.Fatal("未登记能力却查到了台账")
	}
}
