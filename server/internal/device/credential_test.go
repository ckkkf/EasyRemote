package device

import (
	"context"
	"regexp"
	"testing"
	"time"
)

// ER-403：验证码必须 6 位数字、10 分钟有效，轮换后旧码立即失效。
func TestVerificationCodeFormatAndRotation(t *testing.T) {
	s, _, code := setup(t)
	now := time.Now().UTC()
	s.SetClock(func() time.Time { return now })

	first, exp, err := s.RotateCode(context.Background(), code)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile("^[0-9]{6}$").MatchString(first) {
		t.Fatalf("code=%q, want 6 digits", first)
	}
	if exp.Sub(now) < 9*time.Minute || exp.Sub(now) > 11*time.Minute {
		t.Fatalf("ttl=%v, want ~10m", exp.Sub(now))
	}

	second, _, err := s.RotateCode(context.Background(), code)
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatalf("rotation produced same code %q", first)
	}
	if err := s.VerifyCode(context.Background(), code, first); err == nil {
		t.Fatal("old code must be invalid after rotation")
	}
	if err := s.VerifyCode(context.Background(), code, second); err != nil {
		t.Fatalf("new code rejected: %v", err)
	}
}

// 连续生成不应出现固定值/明显重复（CSPRNG 而非时间/PID）。
func TestVerificationCodeRandomSource(t *testing.T) {
	s, _, code := setup(t)
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		v, _, err := s.RotateCode(context.Background(), code)
		if err != nil {
			t.Fatal(err)
		}
		if seen[v] {
			t.Fatalf("duplicate code %q at %d", v, i)
		}
		seen[v] = true
	}
}

// 固定密码：6..16 位字母数字；不接受空/短/符号；服务端只存哈希。
func TestFixedPasswordValidation(t *testing.T) {
	s, mem, code := setup(t)
	ctx := context.Background()
	dev, err := mem.GetDeviceByCode(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "12345", "12345678901234567", "abc-123", "密码123456"} {
		if err := s.SetPassword(ctx, dev.ID, bad); err == nil {
			t.Fatalf("password %q must be rejected", bad)
		}
	}
	if err := s.SetPassword(ctx, dev.ID, "aBc12345"); err != nil {
		t.Fatal(err)
	}
	d, err := mem.GetDeviceByCode(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	if d.PasswordHash == "" || d.PasswordHash == "aBc12345" {
		t.Fatalf("password must be hashed, got %q", d.PasswordHash)
	}
	if !s.CheckPassword(ctx, code, "aBc12345") {
		t.Fatal("correct password rejected")
	}
	if s.CheckPassword(ctx, code, "aBc12346") {
		t.Fatal("wrong password accepted")
	}
}
