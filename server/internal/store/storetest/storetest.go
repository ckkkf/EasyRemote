// Package storetest 是 Store 的契约测试：内存实现与 PG 实现必须跑同一套用例。
package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/easyremote/server/internal/store"
)

// Factory 创建一份干净的 Store。
type Factory func(t *testing.T) store.Store

// Run 跑完整契约。
func Run(t *testing.T, newStore Factory) {
	t.Run("AccountLifecycle", func(t *testing.T) { testAccountLifecycle(t, newStore(t)) })
	t.Run("AccountUniquePhone", func(t *testing.T) { testAccountUniquePhone(t, newStore(t)) })
	t.Run("DeviceLifecycle", func(t *testing.T) { testDeviceLifecycle(t, newStore(t)) })
	t.Run("DeviceCodeUnique", func(t *testing.T) { testDeviceCodeUnique(t, newStore(t)) })
	t.Run("VerifyCodeAtomic", func(t *testing.T) { testVerifyCodeAtomic(t, newStore(t)) })
	t.Run("TrustExpiry", func(t *testing.T) { testTrustExpiry(t, newStore(t)) })
	t.Run("SessionLifecycle", func(t *testing.T) { testSessionLifecycle(t, newStore(t)) })
	t.Run("AuditOrder", func(t *testing.T) { testAuditOrder(t, newStore(t)) })
	t.Run("FileLifecycle", func(t *testing.T) { testFileLifecycle(t, newStore(t)) })
	t.Run("AssistSingleUse", func(t *testing.T) { testAssistSingleUse(t, newStore(t)) })
	t.Run("ConfigRoundTrip", func(t *testing.T) { testConfigRoundTrip(t, newStore(t)) })
	t.Run("Pagination", func(t *testing.T) { testPagination(t, newStore(t)) })
	t.Run("NotFoundIsDistinct", func(t *testing.T) { testNotFound(t, newStore(t)) })
}

func bg() context.Context { return context.Background() }

func testAccountLifecycle(t *testing.T, s store.Store) {
	a := &store.Account{PhoneHash: "hash-1", PhoneEnc: "13800008882", Status: "active"}
	if err := s.CreateAccount(bg(), a); err != nil {
		t.Fatalf("create: %v", err)
	}
	if a.ID == "" {
		t.Fatal("id not assigned")
	}
	got, err := s.GetAccount(bg(), a.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != "active" {
		t.Fatalf("status = %q", got.Status)
	}
	if err := s.UpdateAccountStatus(bg(), a.ID, "banned"); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = s.GetAccount(bg(), a.ID)
	if got.Status != "banned" {
		t.Fatalf("status not updated: %q", got.Status)
	}
	byPhone, err := s.GetAccountByPhoneHash(bg(), "hash-1")
	if err != nil || byPhone.ID != a.ID {
		t.Fatalf("by phone: %v %v", byPhone, err)
	}
}

func testAccountUniquePhone(t *testing.T, s store.Store) {
	if err := s.CreateAccount(bg(), &store.Account{PhoneHash: "dup"}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateAccount(bg(), &store.Account{PhoneHash: "dup"}); err != store.ErrConflict {
		t.Fatalf("want ErrConflict, got %v", err)
	}
}

func testDeviceLifecycle(t *testing.T, s store.Store) {
	a := &store.Account{PhoneHash: "dev-acc"}
	if err := s.CreateAccount(bg(), a); err != nil {
		t.Fatal(err)
	}
	d := &store.Device{DeviceCode: "842615037", AccountID: a.ID, Platform: "windows", Online: true}
	if err := s.CreateDevice(bg(), d); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.GetDeviceByCode(bg(), "842615037")
	if err != nil || got.ID != d.ID {
		t.Fatalf("by code: %v %v", got, err)
	}
	if err := s.UpdateDeviceToken(bg(), d.ID, "tok-hash"); err != nil {
		t.Fatalf("token: %v", err)
	}
	got, _ = s.GetDevice(bg(), d.ID)
	if got.TokenHash != "tok-hash" {
		t.Fatalf("token not stored: %q", got.TokenHash)
	}
	if err := s.UpdateDeviceSeen(bg(), d.ID, time.Now().UTC(), false); err != nil {
		t.Fatalf("seen: %v", err)
	}
	got, _ = s.GetDevice(bg(), d.ID)
	if got.Online {
		t.Fatal("online should be false")
	}
	list, err := s.ListDevicesByAccount(bg(), a.ID, store.Page{Limit: 10})
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %v", list, err)
	}
}

func testDeviceCodeUnique(t *testing.T, s store.Store) {
	a := &store.Account{PhoneHash: "u1"}
	b := &store.Account{PhoneHash: "u2"}
	if err := s.CreateAccount(bg(), a); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateAccount(bg(), b); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateDevice(bg(), &store.Device{DeviceCode: "111111111", AccountID: a.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateDevice(bg(), &store.Device{DeviceCode: "111111111", AccountID: b.ID}); err != store.ErrConflict {
		t.Fatalf("want ErrConflict, got %v", err)
	}
}

func testVerifyCodeAtomic(t *testing.T, s store.Store) {
	now := time.Now().UTC()
	if err := s.UpsertVerifyCode(bg(), &store.VerifyCode{
		DeviceCode: "dev-1", CodeHash: "ch", ExpiresAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		res, err := s.VerifyAndConsume(bg(), "dev-1", "wrong", now, 5, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if res.Outcome != store.VerifyInvalid {
			t.Fatalf("attempt %d: outcome=%v", i+1, res.Outcome)
		}
	}
	res, _ := s.VerifyAndConsume(bg(), "dev-1", "wrong", now, 5, time.Minute)
	if res.Outcome != store.VerifyLocked {
		t.Fatalf("want locked, got %v", res.Outcome)
	}
	res, _ = s.VerifyAndConsume(bg(), "dev-1", "ch", now, 5, time.Minute)
	if res.Outcome != store.VerifyLocked {
		t.Fatalf("correct code during lock must fail, got %v", res.Outcome)
	}
}

func testTrustExpiry(t *testing.T, s store.Store) {
	now := time.Now().UTC()
	if err := s.UpsertTrust(bg(), &store.TrustEdge{ControllerID: "a", TargetID: "b", ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	ok, err := s.HasTrust(bg(), "a", "b", now)
	if err != nil || !ok {
		t.Fatalf("trust: %v %v", ok, err)
	}
	ok, _ = s.HasTrust(bg(), "a", "b", now.Add(2*time.Hour))
	if ok {
		t.Fatal("trust should expire")
	}
}

func testSessionLifecycle(t *testing.T, s store.Store) {
	now := time.Now().UTC()
	sess := &store.Session{ControllerID: "c", TargetID: "tg", State: "requested", StartedAt: now}
	if err := s.CreateSession(bg(), sess); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSession(bg(), sess.ID)
	if err != nil || got.State != "requested" {
		t.Fatalf("get: %v %v", got, err)
	}
	got.State = "authorized"
	got.AuthExpireAt = now.Add(12 * time.Hour)
	if err := s.UpdateSession(bg(), got); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListSessions(bg(), "authorized", store.Page{Limit: 10})
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %v", list, err)
	}
}

func testAuditOrder(t *testing.T, s store.Store) {
	for i := 0; i < 3; i++ {
		if err := s.AppendAudit(bg(), &store.AuditLog{
			Actor: "root", Role: "super", Action: "test", Object: "o", Result: "ok", TS: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	logs, err := s.ListAudit(bg(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 {
		t.Fatalf("want 2, got %d", len(logs))
	}
}

func testPagination(t *testing.T, s store.Store) {
	for i := 0; i < 5; i++ {
		if err := s.CreateAccount(bg(), &store.Account{PhoneHash: "p" + string(rune('a'+i))}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.ListAccounts(bg(), store.Page{Limit: 2, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 {
		t.Fatalf("want 2, got %d", len(page))
	}
}

func testNotFound(t *testing.T, s store.Store) {
	if _, err := s.GetAccount(bg(), "missing"); err != store.ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if _, err := s.GetDevice(bg(), "missing"); err != store.ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := s.UpdateAccountStatus(bg(), "missing", "banned"); err != store.ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
