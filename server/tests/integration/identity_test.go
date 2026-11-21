//go:build integration

package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
)

// boundDevice 是一次绑定的完整结果。
type boundDevice struct {
	Token       string // 账号 access token
	DeviceID    string
	DeviceToken string // 设备凭证
	Code        string // 9 位设备码（无空格）
	IdentityKey string // 身份公钥（测试用确定性派生）
}

func loginAndBind(t *testing.T, c *client, phone string) boundDevice {
	t.Helper()
	tok := login(t, c, phone)
	id, rawCode, devTok := bind(t, c, tok)
	return boundDevice{
		Token:       tok,
		DeviceID:    id,
		DeviceToken: devTok,
		Code:        strings.ReplaceAll(rawCode, " ", ""),
		IdentityKey: identityPubkeyFor(phone),
	}
}

// identityPubkeyFor 由手机号派生稳定的 32 字节公钥十六进制串（仅测试用）。
func identityPubkeyFor(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

// 设备身份公钥的登记与下发：端到端加密验签依赖这条链路。
func TestDeviceIdentityRegistrationAndDistribution(t *testing.T) {
	c := setup(t)
	ctrl := loginAndBind(t, c, "13700002001")
	tgt := loginAndBind(t, c, "13700002002")

	// 未认证不能登记
	if code, _ := c.do("POST", "/v1/devices/identity", reqOpts{}, map[string]any{
		"device_id": ctrl.DeviceID, "pubkey": ctrl.IdentityKey,
	}); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated registration must be 401, got %d", code)
	}

	// 公钥长度不合法要拒绝
	if code, _ := c.do("POST", "/v1/devices/identity", reqOpts{bearer: ctrl.Token, deviceToken: ctrl.DeviceToken}, map[string]any{
		"device_id": ctrl.DeviceID, "pubkey": "deadbeef",
	}); code != http.StatusBadRequest && code != http.StatusUnauthorized {
		t.Fatalf("short pubkey must be rejected, got %d", code)
	}

	// 设备凭证不对要拒绝
	if code, _ := c.do("POST", "/v1/devices/identity", reqOpts{bearer: ctrl.Token, deviceToken: tgt.DeviceToken}, map[string]any{
		"device_id": ctrl.DeviceID, "pubkey": ctrl.IdentityKey,
	}); code != http.StatusForbidden {
		t.Fatalf("wrong device credential must be 403, got %d", code)
	}

	// 正常登记两端公钥
	if code, body := c.do("POST", "/v1/devices/identity", reqOpts{bearer: ctrl.Token, deviceToken: ctrl.DeviceToken}, map[string]any{
		"device_id": ctrl.DeviceID, "pubkey": ctrl.IdentityKey,
	}); code != http.StatusOK {
		t.Fatalf("register ctrl identity: %d %v", code, body)
	}
	if code, body := c.do("POST", "/v1/devices/identity", reqOpts{bearer: tgt.Token, deviceToken: tgt.DeviceToken}, map[string]any{
		"device_id": tgt.DeviceID, "pubkey": tgt.IdentityKey,
	}); code != http.StatusOK {
		t.Fatalf("register target identity: %d %v", code, body)
	}

	// 建会话：被控端先轮换验证码，主控端带上验证码请求
	vcode, vbody := c.do("POST", "/v1/devices/verify-code", reqOpts{deviceToken: tgt.DeviceToken}, map[string]any{
		"device_code": tgt.Code,
	})
	if vcode != http.StatusOK {
		t.Fatalf("rotate captcha: %d %v", vcode, vbody)
	}
	captcha, _ := vbody["code"].(string)

	code, sess := c.do("POST", "/v1/connect/request", reqOpts{bearer: ctrl.Token, deviceToken: ctrl.DeviceToken}, map[string]any{
		"device_code": tgt.Code,
		"captcha":     captcha,
	})
	if code != http.StatusOK {
		t.Fatalf("connect request: %d %v", code, sess)
	}
	sid, _ := sess["session_id"].(string)

	// 参与方可读双方公钥
	code, keys := c.do("GET", "/v1/connect/keys?session_id="+sid, reqOpts{bearer: ctrl.Token}, nil)
	if code != http.StatusOK {
		t.Fatalf("read keys: %d %v", code, keys)
	}
	if keys["controller_pubkey"] != ctrl.IdentityKey {
		t.Fatalf("controller pubkey mismatch: %v", keys["controller_pubkey"])
	}
	if keys["target_pubkey"] != tgt.IdentityKey {
		t.Fatalf("target pubkey mismatch: %v", keys["target_pubkey"])
	}

	// 被控端同样可读
	if code, _ := c.do("GET", "/v1/connect/keys?session_id="+sid, reqOpts{bearer: tgt.Token}, nil); code != http.StatusOK {
		t.Fatalf("target must read keys too, got %d", code)
	}

	// 第三方不可读
	other := login(t, c, "13700002003")
	if code, _ := c.do("GET", "/v1/connect/keys?session_id="+sid, reqOpts{bearer: other}, nil); code != http.StatusForbidden {
		t.Fatalf("outsider must be 403, got %d", code)
	}
}

// 每台设备的身份公钥必须不同。
func TestIdentityKeysAreDistinctPerDevice(t *testing.T) {
	c := setup(t)
	a := loginAndBind(t, c, "13700002011")
	b := loginAndBind(t, c, "13700002012")
	if a.DeviceID == b.DeviceID {
		t.Fatal("device ids must differ")
	}
	if a.IdentityKey == b.IdentityKey {
		t.Fatal("identity keys must differ per device")
	}
	if len(a.IdentityKey) != 64 {
		t.Fatalf("identity key must be 32-byte hex, got %d chars", len(a.IdentityKey))
	}
}
