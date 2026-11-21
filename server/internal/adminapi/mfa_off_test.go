package adminapi

import (
	"context"
	"testing"

	"github.com/easyremote/server/internal/errcode"
)

// 开发开关打开后：空验证码也能登录（本地联调免二次验证）。
func TestDevSkipTOTPAllowsEmptyCode(t *testing.T) {
	svc, _ := setupTOTP(t)
	svc.SetDevSkipTOTP(true)
	u, err := svc.Authenticate(context.Background(), "root", "pw", "")
	if err != nil {
		t.Fatalf("skip totp should allow empty code: %v", err)
	}
	if u.Username != "root" {
		t.Fatalf("unexpected user %q", u.Username)
	}
}

// 开关只跳过二次验证，口令错误仍然必须拒。
func TestDevSkipTOTPStillChecksPassword(t *testing.T) {
	svc, _ := setupTOTP(t)
	svc.SetDevSkipTOTP(true)
	if _, err := svc.Authenticate(context.Background(), "root", "wrong", ""); !errcode.Is(err, errcode.AuthInvalidCredential) {
		t.Fatalf("want invalid credential, got %v", err)
	}
}

// 未打开开关时仍然强制二次验证：开关不能默认生效。
func TestTOTPStillRequiredByDefault(t *testing.T) {
	svc, _ := setupTOTP(t)
	if _, err := svc.Authenticate(context.Background(), "root", "pw", ""); !errcode.Is(err, errcode.AdminMfaRequired) {
		t.Fatalf("want MFA required by default, got %v", err)
	}
}
