package gateway

import (
	"net/http"
	"strings"
	"testing"
)

// 空 platform 不得被兜底成 "windows"。
//
// 历史 bug：/v1/devices/bind 里 `_ = httpx.Decode(...)` 吞掉解析错误，再用
// `if req.Platform == "" { req.Platform = "windows" }` 兜底，于是任何没带
// platform 的调用都会注册成一台 Windows 设备，客户端列表里就出现
// 「macOS 机器显示成 Windows 设备」。空值只允许落成 unknown。
func TestBindEmptyPlatformIsUnknownNotWindows(t *testing.T) {
	f := newFixture(t)
	hdr := map[string]string{"Authorization": "Bearer " + f.acct.Token}

	var body map[string]any
	if status := f.postJSON("/v1/devices/bind", map[string]any{}, hdr, &body); status != http.StatusOK {
		t.Fatalf("空 platform 应正常绑定（记成 unknown），实际 HTTP %d body=%v", status, body)
	}
	if got := str(body["platform"]); got != "unknown" {
		t.Fatalf("空 platform 必须落成 unknown，实际 %q（body=%v）", got, body)
	}
}

// GW-04 必须回显服务端落库的平台真值：客户端拿它显示，不再用自己上报的字符串。
func TestBindEchoesNormalizedPlatform(t *testing.T) {
	f := newFixture(t)
	hdr := map[string]string{"Authorization": "Bearer " + f.acct.Token}

	var body map[string]any
	f.postJSON("/v1/devices/bind", map[string]any{"platform": "macOS"}, hdr, &body)

	if got := str(body["platform"]); got != "macos" {
		t.Fatalf("响应 platform 应为归一化后的 macos，实际 %q（body=%v）", got, body)
	}
	// 再从 GET /v1/devices 确认落库值一致（响应回显和列表不能是两套口径）。
	code := str(body["raw_code"])
	status, raw := f.do(http.MethodGet, "/v1/devices", nil, map[string]string{
		"Authorization": "Bearer " + f.acct.Token,
	})
	if status != http.StatusOK {
		t.Fatalf("list devices: HTTP %d %s", status, raw)
	}
	var list struct {
		Devices []struct {
			RawCode  string `json:"raw_code"`
			Platform string `json:"platform"`
		} `json:"devices"`
	}
	if err := unmarshalData(raw, &list); err != nil {
		t.Fatalf("decode devices: %v (%s)", err, raw)
	}
	for _, d := range list.Devices {
		if d.RawCode == code {
			if d.Platform != "macos" {
				t.Fatalf("列表里的 platform 应为 macos，实际 %q", d.Platform)
			}
			return
		}
	}
	t.Fatalf("新绑的设备 %s 没出现在列表里：%s", code, raw)
}

// deviceCount 从 GET /v1/devices 数设备（账号来自令牌，不靠测试夹具的 AccountID）。
func deviceCount(t *testing.T, f *fixture) int {
	t.Helper()
	status, raw := f.do(http.MethodGet, "/v1/devices", nil, map[string]string{
		"Authorization": "Bearer " + f.acct.Token,
	})
	if status != http.StatusOK {
		t.Fatalf("list devices: HTTP %d %s", status, raw)
	}
	var list struct {
		Devices []struct {
			RawCode string `json:"raw_code"`
		} `json:"devices"`
	}
	if err := unmarshalData(raw, &list); err != nil {
		t.Fatalf("decode devices: %v (%s)", err, raw)
	}
	return len(list.Devices)
}

// 带上本机已有身份时，GW-04 必须复用同一台设备：设备码不变、平台刷新、凭证轮换，
// 且再绑一次行数不增加。这是「每登录一次多一台幽灵设备」的止血点。
func TestBindReusesDeviceIdentity(t *testing.T) {
	f := newFixture(t) // 夹具已经绑过一台（platform=test）
	hdr := map[string]string{"Authorization": "Bearer " + f.acct.Token}

	var first map[string]any
	f.postJSON("/v1/devices/bind", map[string]any{
		"platform":     "macOS",
		"device_id":    f.acct.DeviceID,
		"device_token": f.acct.DeviceToken,
	}, hdr, &first)

	if got := str(first["raw_code"]); got != f.acct.DeviceCode {
		t.Fatalf("复用失败：设备码从 %q 变成 %q", f.acct.DeviceCode, got)
	}
	if got := str(first["platform"]); got != "macos" {
		t.Fatalf("platform 应刷新成 macos，实际 %q", got)
	}
	newToken := str(first["device_token"])
	if newToken == "" || newToken == f.acct.DeviceToken {
		t.Fatalf("复用时应轮换设备凭证，实际 %q", newToken)
	}
	if n := deviceCount(t, f); n != 1 {
		t.Fatalf("幂等绑定后设备数应为 1，实际 %d", n)
	}

	// 换平台再绑一次：还是同一台，平台跟着改。
	var second map[string]any
	f.postJSON("/v1/devices/bind", map[string]any{
		"platform":     "windows",
		"device_id":    f.acct.DeviceID,
		"device_token": newToken,
	}, hdr, &second)
	if got := str(second["raw_code"]); got != f.acct.DeviceCode {
		t.Fatalf("二次复用失败：设备码 %q", got)
	}
	if got := str(second["platform"]); got != "windows" {
		t.Fatalf("platform 应刷新成 windows，实际 %q", got)
	}
	if n := deviceCount(t, f); n != 1 {
		t.Fatalf("二次幂等绑定后设备数应为 1，实际 %d", n)
	}
}

// 凭证对不上时不许复用别人的设备行：宁可新建，也不能顶替。
func TestBindWithBadDeviceTokenCreatesNewDevice(t *testing.T) {
	f := newFixture(t)
	hdr := map[string]string{"Authorization": "Bearer " + f.acct.Token}

	var body map[string]any
	f.postJSON("/v1/devices/bind", map[string]any{
		"platform":     "macos",
		"device_id":    f.acct.DeviceID,
		"device_token": "bogus-token",
	}, hdr, &body)

	if got := str(body["raw_code"]); got == f.acct.DeviceCode {
		t.Fatalf("凭证无效却复用了已有设备行：%q", got)
	}
	if n := deviceCount(t, f); n != 2 {
		t.Fatalf("凭证无效应新建一台（1→2），实际 %d", n)
	}
}

// 请求体解析失败必须报错，且不许凭空多出一台设备。
func TestBindMalformedBodyIsRejected(t *testing.T) {
	f := newFixture(t)
	hdr := map[string]string{
		"Authorization": "Bearer " + f.acct.Token,
		"Content-Type":  "application/json",
	}
	before, err := f.app.Device.List(t.Context(), f.acct.AccountID)
	if err != nil {
		t.Fatalf("list devices: %v", err)
	}

	// DisallowUnknownFields：多一个字段整包作废。
	status, raw := f.do(http.MethodPost, "/v1/devices/bind",
		strings.NewReader(`{"platform":"macos","device_name":"mac"}`), hdr)
	if status == http.StatusOK {
		t.Fatalf("解析失败的 bind 不许成功，否则会凭空多一台设备：%s", raw)
	}

	after, err := f.app.Device.List(t.Context(), f.acct.AccountID)
	if err != nil {
		t.Fatalf("list devices: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("解析失败仍创建设备：%d → %d", len(before), len(after))
	}
}
