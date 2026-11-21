package device

import (
	"context"
	"testing"
	"time"

	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/store"
)

func setup(t *testing.T) (*Service, *store.Memory, string) {
	t.Helper()
	mem := store.NewMemory()
	if err := mem.CreateAccount(context.Background(), &store.Account{ID: "acc1", PhoneHash: "h", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	s := New(mem, []byte("k"))
	d, _, err := s.Bind(context.Background(), "acc1", "windows")
	if err != nil {
		t.Fatal(err)
	}
	return s, mem, d.DeviceCode
}

func TestBindProducesNineDigitUniqueCode(t *testing.T) {
	s, mem, code := setup(t)
	if len(code) != 9 {
		t.Fatalf("want 9 digits, got %q", code)
	}
	seen := map[string]bool{code: true}
	for i := 0; i < 50; i++ {
		d, _, err := s.Bind(context.Background(), "acc1", "windows")
		if err != nil {
			t.Fatal(err)
		}
		if seen[d.DeviceCode] {
			t.Fatalf("duplicate code %s", d.DeviceCode)
		}
		seen[d.DeviceCode] = true
	}
	if countDevices(mem) != 51 {
		t.Fatalf("want 51 devices, got %d", countDevices(mem))
	}
}

func TestFormatCode(t *testing.T) {
	if got := FormatCode("842615037"); got != "842 615 037" {
		t.Fatalf("got %q", got)
	}
}

func TestVerifyCodeSuccessConsumesCode(t *testing.T) {
	s, _, code := setup(t)
	v, _, err := s.RotateCode(context.Background(), code)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyCode(context.Background(), code, v); err != nil {
		t.Fatalf("first verify: %v", err)
	}
	// 一次性：第二次必须失败
	if err := s.VerifyCode(context.Background(), code, v); err == nil {
		t.Fatal("want reuse rejected")
	}
}

func TestVerifyCodeWrongLocksAfterFive(t *testing.T) {
	s, _, code := setup(t)
	if _, _, err := s.RotateCode(context.Background(), code); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		err := s.VerifyCode(context.Background(), code, "000000")
		if !errcode.Is(err, errcode.ConnCaptchaInvalid) {
			t.Fatalf("attempt %d: want invalid, got %v", i+1, err)
		}
	}
	_ = s.VerifyCode(context.Background(), code, "000000")
	err := s.VerifyCode(context.Background(), code, "000000")
	if !errcode.Is(err, errcode.ConnCaptchaLocked) {
		t.Fatalf("want locked, got %v", err)
	}
}

func TestVerifyCodeExpired(t *testing.T) {
	s, _, code := setup(t)
	now := time.Now().UTC()
	s.SetClock(func() time.Time { return now })
	v, _, _ := s.RotateCode(context.Background(), code)
	s.SetClock(func() time.Time { return now.Add(11 * time.Minute) })
	if err := s.VerifyCode(context.Background(), code, v); !errcode.Is(err, errcode.ConnCaptchaExpired) {
		t.Fatalf("want expired, got %v", err)
	}
}

func TestTrustExpires(t *testing.T) {
	s, _, _ := setup(t)
	if err := s.GrantTrust(context.Background(), "a", "b"); err != nil {
		t.Fatal(err)
	}
	if !s.HasTrust(context.Background(), "a", "b") {
		t.Fatal("want trust")
	}
	now := time.Now().UTC()
	s.SetClock(func() time.Time { return now.Add(8 * 24 * time.Hour) })
	if s.HasTrust(context.Background(), "a", "b") {
		t.Fatal("want trust expired")
	}
}

func countDevices(m *store.Memory) int {
	list, err := m.ListDevices(context.Background(), store.Page{Limit: 500})
	if err != nil {
		return -1
	}
	return len(list)
}
