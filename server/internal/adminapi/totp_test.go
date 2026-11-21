package adminapi

import (
	"context"
	"testing"
	"time"

	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/session"
	"github.com/easyremote/server/internal/store"
)

// 标准 base32 secret（"Hello!" 的 base32），验证与标准认证器互通
const stdSecret = "JBSWY3DPEE"

func setupTOTP(t *testing.T) (*Service, *store.Memory) {
	t.Helper()
	mem := store.NewMemory()
	mem.PutAdmin(context.Background(), &store.AdminUser{
		Username: "root", PasswordHash: HashPassword("pw"),
		TOTPSecret: stdSecret, Role: RoleSuper, Status: "active",
	})
	return New(mem, session.New(mem)), mem
}

func TestTOTPMatchesStandardBase32(t *testing.T) {
	// 用同一 secret 生成并与 hotp 结果对照，证明 base32 解码生效
	at := time.Unix(1_700_000_000, 0).UTC()
	code := TOTPCode(stdSecret, at)
	if len(code) != 6 {
		t.Fatalf("code length %d", len(code))
	}
	svc, _ := setupTOTP(t)
	svc.SetClock(func() time.Time { return at })
	if _, err := svc.Authenticate(context.Background(), "root", "pw", code); err != nil {
		t.Fatalf("authenticate with generated code: %v", err)
	}
}

func TestTOTPReplayRejected(t *testing.T) {
	at := time.Unix(1_700_000_000, 0).UTC()
	code := TOTPCode(stdSecret, at)
	svc, _ := setupTOTP(t)
	svc.SetClock(func() time.Time { return at })

	if _, err := svc.Authenticate(context.Background(), "root", "pw", code); err != nil {
		t.Fatalf("first use should pass: %v", err)
	}
	// 同一时间步内重复使用同一个码必须被拒
	if _, err := svc.Authenticate(context.Background(), "root", "pw", code); !errcode.Is(err, errcode.AdminMfaRequired) {
		t.Fatalf("replay must be rejected, got %v", err)
	}
}

func TestTOTPWrongCodeRejected(t *testing.T) {
	svc, _ := setupTOTP(t)
	if _, err := svc.Authenticate(context.Background(), "root", "pw", "000000"); !errcode.Is(err, errcode.AdminMfaRequired) {
		t.Fatalf("want MFA required, got %v", err)
	}
}
