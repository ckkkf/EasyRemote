package storetest

import (
	"errors"
	"testing"
	"time"

	"github.com/easyremote/server/internal/store"
)

// testFileLifecycle 覆盖文件元数据的增查列删与账号隔离。
func testFileLifecycle(t *testing.T, s store.Store) {
	ctx := bg()
	acc := &store.Account{PhoneHash: "file-acc"}
	if err := s.CreateAccount(ctx, acc); err != nil {
		t.Fatal(err)
	}
	other := &store.Account{PhoneHash: "file-acc-2"}
	if err := s.CreateAccount(ctx, other); err != nil {
		t.Fatal(err)
	}

	first := &store.FileMeta{
		AccountID: acc.ID, DeviceID: "dev-1", Name: "a.txt",
		Size: 3, SHA256: "aa", StoragePath: "/tmp/a", CreatedAt: time.Now().UTC().Add(-time.Second),
	}
	if err := s.CreateFile(ctx, first); err != nil {
		t.Fatalf("create: %v", err)
	}
	second := &store.FileMeta{
		AccountID: acc.ID, DeviceID: "dev-1", Name: "b.txt",
		Size: 5, SHA256: "bb", StoragePath: "/tmp/b", CreatedAt: time.Now().UTC(),
	}
	if err := s.CreateFile(ctx, second); err != nil {
		t.Fatalf("create: %v", err)
	}
	if first.ID == "" || second.ID == "" {
		t.Fatal("id not assigned")
	}

	got, err := s.GetFile(ctx, second.ID)
	if err != nil || got.Name != "b.txt" || got.Size != 5 {
		t.Fatalf("get: %+v %v", got, err)
	}

	list, err := s.ListFilesByAccount(ctx, acc.ID, store.Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 files, got %d", len(list))
	}
	// 必须按创建时间倒序（新的在前）。
	if list[0].ID != second.ID {
		t.Fatalf("want newest first, got %s", list[0].Name)
	}

	// 账号隔离：别人的列表拿不到我的文件。
	otherList, err := s.ListFilesByAccount(ctx, other.ID, store.Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(otherList) != 0 {
		t.Fatalf("cross-account leak: %d", len(otherList))
	}

	if err := s.DeleteFile(ctx, first.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetFile(ctx, first.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound after delete, got %v", err)
	}
	if err := s.DeleteFile(ctx, first.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second delete should report ErrNotFound, got %v", err)
	}
}

// testAssistSingleUse 覆盖协助码一次性语义（并发安全由实现的原子原语保证）。
func testAssistSingleUse(t *testing.T, s store.Store) {
	ctx := bg()
	now := time.Now().UTC()
	code := &store.AssistCode{
		Code: "123456", AccountID: "acc", TargetID: "dev",
		CreatedAt: now, ExpiresAt: now.Add(10 * time.Minute),
	}
	if err := s.PutAssistCode(ctx, code); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetAssistCode(ctx, "123456")
	if err != nil || got.TargetID != "dev" || got.Redeemed() {
		t.Fatalf("get: %+v %v", got, err)
	}
	ok, err := s.RedeemAssistCode(ctx, "123456", now)
	if err != nil || !ok {
		t.Fatalf("first redeem: %v %v", ok, err)
	}
	ok, err = s.RedeemAssistCode(ctx, "123456", now)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("second redeem must fail")
	}
	got, _ = s.GetAssistCode(ctx, "123456")
	if !got.Redeemed() {
		t.Fatal("code should be marked redeemed")
	}

	// 过期码不可兑换。
	expired := &store.AssistCode{
		Code: "654321", AccountID: "acc", TargetID: "dev",
		CreatedAt: now.Add(-20 * time.Minute), ExpiresAt: now.Add(-time.Minute),
	}
	if err := s.PutAssistCode(ctx, expired); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.RedeemAssistCode(ctx, "654321", now); ok {
		t.Fatal("expired code must not redeem")
	}
	// 不存在的码。
	if ok, _ := s.RedeemAssistCode(ctx, "999999", now); ok {
		t.Fatal("unknown code must not redeem")
	}
}

// testConfigRoundTrip 覆盖配置键值读写。
func testConfigRoundTrip(t *testing.T, s store.Store) {
	ctx := bg()
	got, err := s.ListConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("fresh store should have no config, got %v", got)
	}
	if err := s.PutConfig(ctx, "diag.auto_refresh_secs", "15"); err != nil {
		t.Fatal(err)
	}
	if err := s.PutConfig(ctx, "diag.auto_refresh_secs", "20"); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	got, err = s.ListConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got["diag.auto_refresh_secs"] != "20" {
		t.Fatalf("value = %q", got["diag.auto_refresh_secs"])
	}
}
