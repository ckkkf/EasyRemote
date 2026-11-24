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
	t.Run("AccountPhoneEncBackfill", func(t *testing.T) { testUpdateAccountPhoneEnc(t, newStore(t)) })
	t.Run("AccountLastLogin", func(t *testing.T) { testTouchAccountLogin(t, newStore(t)) })
	t.Run("DeviceLifecycle", func(t *testing.T) { testDeviceLifecycle(t, newStore(t)) })
	t.Run("DeviceCodeUnique", func(t *testing.T) { testDeviceCodeUnique(t, newStore(t)) })
	t.Run("DeviceMeta", func(t *testing.T) { testUpdateDeviceMeta(t, newStore(t)) })
	t.Run("DeviceRisk", func(t *testing.T) { testSetDeviceRisk(t, newStore(t)) })
	t.Run("DeviceDeleteAndOnlineFilter", func(t *testing.T) { testDeviceDeleteAndOnlineFilter(t, newStore(t)) })
	t.Run("DeviceCountsByAccount", func(t *testing.T) { testDeviceCountsByAccount(t, newStore(t)) })
	t.Run("VerifyCodeAtomic", func(t *testing.T) { testVerifyCodeAtomic(t, newStore(t)) })
	t.Run("TrustExpiry", func(t *testing.T) { testTrustExpiry(t, newStore(t)) })
	t.Run("SessionLifecycle", func(t *testing.T) { testSessionLifecycle(t, newStore(t)) })
	t.Run("AuditOrder", func(t *testing.T) { testAuditOrder(t, newStore(t)) })
	t.Run("AuditFiltersAndActions", func(t *testing.T) { testAuditFiltersAndActions(t, newStore(t)) })
	t.Run("FileLifecycle", func(t *testing.T) { testFileLifecycle(t, newStore(t)) })
	t.Run("AssistSingleUse", func(t *testing.T) { testAssistSingleUse(t, newStore(t)) })
	t.Run("ConfigRoundTrip", func(t *testing.T) { testConfigRoundTrip(t, newStore(t)) })
	t.Run("AdminManagement", func(t *testing.T) { testAdminManagement(t, newStore(t)) })
	t.Run("ClientVersions", func(t *testing.T) { testClientVersions(t, newStore(t)) })
	t.Run("BlacklistAndAlerts", func(t *testing.T) { testBlacklistAndAlerts(t, newStore(t)) })
	t.Run("QualitySamples", func(t *testing.T) { testQualitySamples(t, newStore(t)) })
	t.Run("AggregateDeviceQuality", func(t *testing.T) { testAggregateDeviceQuality(t, newStore(t)) })
	t.Run("QualityStatsSince", func(t *testing.T) { testQualityStatsSince(t, newStore(t)) })
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
	if err := s.UpdateDevicePlatform(bg(), d.ID, "macos"); err != nil {
		t.Fatalf("platform: %v", err)
	}
	got, _ = s.GetDevice(bg(), d.ID)
	if got.Platform != "macos" {
		t.Fatalf("platform not stored: %q", got.Platform)
	}
	list, err := s.ListDevicesByAccount(bg(), a.ID, store.Page{Limit: 10})
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %v", list, err)
	}
	// 未知 id 必须报 ErrNotFound，不能静默成功。
	if err := s.UpdateDevicePlatform(bg(), "dev-does-not-exist", "linux"); err != store.ErrNotFound {
		t.Fatalf("unknown device should be ErrNotFound, got %v", err)
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

// ADM-02b：手机号密文可回填（历史账号登录时补写）。
func testUpdateAccountPhoneEnc(t *testing.T, s store.Store) {
	a := &store.Account{PhoneHash: "hash-enc", Status: "active"}
	if err := s.CreateAccount(bg(), a); err != nil {
		t.Fatalf("create: %v", err)
	}
	if a.PhoneEnc != "" {
		t.Fatalf("precondition: want empty PhoneEnc, got %q", a.PhoneEnc)
	}
	if err := s.UpdateAccountPhoneEnc(bg(), a.ID, "13800008882"); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := s.GetAccount(bg(), a.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.PhoneEnc != "13800008882" {
		t.Fatalf("want backfilled phone, got %q", got.PhoneEnc)
	}
	// 不存在的账号必须返回 ErrNotFound，而不是静默成功。
	if err := s.UpdateAccountPhoneEnc(bg(), "no-such-account", "x"); err == nil {
		t.Fatal("want ErrNotFound for unknown account")
	}
}

// ADM-API-03a：一次查多个账号的设备数（后台用户列表列）。
func testDeviceCountsByAccount(t *testing.T, s store.Store) {
	for _, a := range []*store.Account{
		{ID: "acc-x", PhoneHash: "hx", PhoneEnc: "13800000011", Status: "active"},
		{ID: "acc-y", PhoneHash: "hy", PhoneEnc: "13800000012", Status: "active"},
		{ID: "acc-z", PhoneHash: "hz", PhoneEnc: "13800000013", Status: "active"},
	} {
		if err := s.CreateAccount(bg(), a); err != nil {
			t.Fatalf("create account: %v", err)
		}
	}
	for _, d := range []*store.Device{
		{ID: "dx1", DeviceCode: "900000001", AccountID: "acc-x", Platform: "macOS"},
		{ID: "dx2", DeviceCode: "900000002", AccountID: "acc-x", Platform: "Windows"},
		{ID: "dy1", DeviceCode: "900000003", AccountID: "acc-y", Platform: "Android"},
	} {
		if err := s.CreateDevice(bg(), d); err != nil {
			t.Fatalf("create device: %v", err)
		}
	}
	counts, err := s.DeviceCountsByAccount(bg(), []string{"acc-x", "acc-y", "acc-z"})
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if counts["acc-x"] != 2 || counts["acc-y"] != 1 {
		t.Fatalf("counts: %+v", counts)
	}
	if _, ok := counts["acc-z"]; ok {
		t.Fatalf("没有设备的账号不应出现在结果里: %+v", counts)
	}
	empty, err := s.DeviceCountsByAccount(bg(), nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("空入参: %v %+v", err, empty)
	}
}

// ADM-API-03b：最近登录时间由登录路径写入。
func testTouchAccountLogin(t *testing.T, s store.Store) {
	a := &store.Account{PhoneHash: "hash-login", PhoneEnc: "13800000021", Status: "active"}
	if err := s.CreateAccount(bg(), a); err != nil {
		t.Fatalf("create: %v", err)
	}
	at := time.Date(2026, 11, 23, 4, 0, 0, 0, time.UTC)
	if err := s.TouchAccountLogin(bg(), a.ID, at); err != nil {
		t.Fatalf("touch: %v", err)
	}
	got, err := s.GetAccount(bg(), a.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.LastLoginAt.Equal(at) {
		t.Fatalf("want %v got %v", at, got.LastLoginAt)
	}
	if err := s.TouchAccountLogin(bg(), "no-such", at); err == nil {
		t.Fatal("want ErrNotFound for unknown account")
	}
}

// ADM-API-04a：设备元信息可写入，且空值不覆盖已有值。
func testUpdateDeviceMeta(t *testing.T, s store.Store) {
	d := &store.Device{DeviceCode: "910000001", AccountID: "acc-meta", Platform: "macOS"}
	if err := s.CreateDevice(bg(), d); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.UpdateDeviceMeta(bg(), d.ID, "陈默的工作站", "macOS 15.1", "0.2.12", "symmetric"); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := s.GetDevice(bg(), d.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.DeviceName != "陈默的工作站" || got.OSVersion != "macOS 15.1" || got.ClientVersion != "0.2.12" || got.NATType != "symmetric" {
		t.Fatalf("meta not stored: %+v", got)
	}
	// 只上报一个字段：其余保留旧值。
	if err := s.UpdateDeviceMeta(bg(), d.ID, "", "", "0.2.13", ""); err != nil {
		t.Fatalf("partial update: %v", err)
	}
	got, _ = s.GetDevice(bg(), d.ID)
	if got.DeviceName != "陈默的工作站" || got.OSVersion != "macOS 15.1" || got.ClientVersion != "0.2.13" || got.NATType != "symmetric" {
		t.Fatalf("partial update clobbered fields: %+v", got)
	}
	if err := s.UpdateDeviceMeta(bg(), "no-such", "x", "", "", ""); err == nil {
		t.Fatal("want ErrNotFound for unknown device")
	}
}

// ADM-API-04b：设备在线过滤与解绑。
func testDeviceDeleteAndOnlineFilter(t *testing.T, s store.Store) {
	on := &store.Device{DeviceCode: "920000001", AccountID: "acc-del", Platform: "macOS", Online: true}
	off := &store.Device{DeviceCode: "920000002", AccountID: "acc-del", Platform: "Windows", Online: false}
	for _, d := range []*store.Device{on, off} {
		if err := s.CreateDevice(bg(), d); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	yes := true
	onlyOnline, err := s.ListDevices(bg(), store.Page{Limit: 10, Online: &yes})
	if err != nil {
		t.Fatalf("list online: %v", err)
	}
	for _, d := range onlyOnline {
		if !d.Online {
			t.Fatalf("online 过滤返回了离线设备: %+v", d)
		}
	}
	if n, err := s.CountDevices(bg(), store.Page{Online: &yes}); err != nil || n != 1 {
		t.Fatalf("count online: n=%d err=%v", n, err)
	}

	if err := s.DeleteDevice(bg(), off.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetDevice(bg(), off.ID); err == nil {
		t.Fatal("deleted device should be gone")
	}
	if err := s.DeleteDevice(bg(), off.ID); err == nil {
		t.Fatal("want ErrNotFound for already deleted device")
	}
}

// ADM-API-05a：审计筛选（动作/结果/操作人/对象/时间范围）与合法动作集合。
func testAuditFiltersAndActions(t *testing.T, s store.Store) {
	base := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	entries := []*store.AuditLog{
		{ID: "a1", Actor: "root", Role: "super", Action: "user.ban", Object: "acc-1", Result: "ok", IP: "127.0.0.1", TS: base},
		{ID: "a2", Actor: "ops", Role: "ops", Action: "config.update", Object: "session.max_minutes", Result: "ok", IP: "127.0.0.1", TS: base.Add(time.Hour)},
		{ID: "a3", Actor: "root", Role: "super", Action: "session.kill", Object: "ses-1", Result: "denied", IP: "127.0.0.1", TS: base.Add(2 * time.Hour)},
	}
	for _, e := range entries {
		if err := s.AppendAudit(bg(), e); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	onlyRoot, err := s.ListAuditPage(bg(), store.Page{Limit: 10, Actor: "root"})
	if err != nil {
		t.Fatalf("actor filter: %v", err)
	}
	if len(onlyRoot) != 2 {
		t.Fatalf("actor filter want 2, got %d", len(onlyRoot))
	}
	if n, err := s.CountAudit(bg(), store.Page{Actor: "root"}); err != nil || n != 2 {
		t.Fatalf("count actor filter: n=%d err=%v", n, err)
	}

	byObject, err := s.ListAuditPage(bg(), store.Page{Limit: 10, Object: "ses-1"})
	if err != nil || len(byObject) != 1 || byObject[0].Action != "session.kill" {
		t.Fatalf("object filter: %v %+v", err, byObject)
	}

	inRange, err := s.ListAuditPage(bg(), store.Page{Limit: 10, From: base.Add(30 * time.Minute), To: base.Add(90 * time.Minute)})
	if err != nil || len(inRange) != 1 || inRange[0].ID != "a2" {
		t.Fatalf("time range filter: %v %+v", err, inRange)
	}

	actions, err := s.ListAuditActions(bg())
	if err != nil {
		t.Fatalf("actions: %v", err)
	}
	want := []string{"config.update", "session.kill", "user.ban"}
	if len(actions) != len(want) {
		t.Fatalf("actions: %+v", actions)
	}
	for i := range want {
		if actions[i] != want[i] {
			t.Fatalf("actions not sorted: %+v", actions)
		}
	}
}

// ADM-API-02c：质量采样只追加、同秒幂等、按时间排序。
func testQualitySamples(t *testing.T, s store.Store) {
	base := time.Date(2026, 11, 23, 5, 0, 0, 0, time.UTC)
	samples := []store.QualitySample{
		{SessionID: "ses-q", TS: base, LatencyMs: 30, LossPermille: 2, Fps: 60, Tier: "hd", Relay: false},
		{SessionID: "ses-q", TS: base.Add(time.Second), LatencyMs: 42, LossPermille: 5, Fps: 58, Tier: "hd", Relay: true},
		// 同一秒重复上报应覆盖而不是重复计数。
		{SessionID: "ses-q", TS: base.Add(time.Second), LatencyMs: 44, LossPermille: 6, Fps: 57, Tier: "hd", Relay: true},
	}
	if err := s.AppendQualitySamples(bg(), samples); err != nil {
		t.Fatalf("append: %v", err)
	}
	got, err := s.ListQualitySamples(bg(), "ses-q", 100)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 samples after dedupe, got %d", len(got))
	}
	if !got[0].TS.Before(got[1].TS) {
		t.Fatalf("samples not sorted: %+v", got)
	}
	if got[1].LatencyMs != 44 || !got[1].Relay {
		t.Fatalf("same-second sample not overwritten: %+v", got[1])
	}
	// 没有采样的会话返回空。
	none, err := s.ListQualitySamples(bg(), "ses-none", 100)
	if err != nil || len(none) != 0 {
		t.Fatalf("empty session: %v %+v", err, none)
	}
}

// ADM-API-02d：设备按天聚合（同一天多条取平均，跨天分组）。
func testAggregateDeviceQuality(t *testing.T, s store.Store) {
	d1 := &store.Device{DeviceCode: "940000001", AccountID: "acc-q", Platform: "macOS"}
	d2 := &store.Device{DeviceCode: "940000002", AccountID: "acc-q", Platform: "Windows"}
	for _, d := range []*store.Device{d1, d2} {
		if err := s.CreateDevice(bg(), d); err != nil {
			t.Fatalf("device: %v", err)
		}
	}
	sess := &store.Session{ID: "ses-q1", ControllerID: d1.ID, TargetID: d2.ID, State: "closed",
		StartedAt: time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)}
	if err := s.CreateSession(bg(), sess); err != nil {
		t.Fatalf("session: %v", err)
	}
	day1 := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	day2 := day1.AddDate(0, 0, 1)
	samples := []store.QualitySample{
		{SessionID: sess.ID, TS: day1, LatencyMs: 20, LossPermille: 2, Fps: 60, Relay: false},
		{SessionID: sess.ID, TS: day1.Add(time.Second), LatencyMs: 30, LossPermille: 4, Fps: 60, Relay: true},
		{SessionID: sess.ID, TS: day2, LatencyMs: 50, LossPermille: 6, Fps: 55, Relay: true},
	}
	if err := s.AppendQualitySamples(bg(), samples); err != nil {
		t.Fatalf("append: %v", err)
	}
	days, err := s.AggregateDeviceQuality(bg(), d1.ID, day1.AddDate(0, 0, -1))
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if len(days) != 2 {
		t.Fatalf("want 2 days, got %+v", days)
	}
	if days[0].Date != "2026-11-20" || days[0].AvgLatencyMs != 25 || days[0].RelayPercent != 50 || days[0].Samples != 2 {
		t.Fatalf("day1: %+v", days[0])
	}
	if days[1].Date != "2026-11-21" || days[1].AvgLatencyMs != 50 || days[1].RelayPercent != 100 {
		t.Fatalf("day2: %+v", days[1])
	}
	// 窗口外不返回。
	empty, err := s.AggregateDeviceQuality(bg(), d1.ID, day2.AddDate(0, 0, 1))
	if err != nil || len(empty) != 0 {
		t.Fatalf("out of window: %v %+v", err, empty)
	}
	// 无关设备没有数据。
	other, err := s.AggregateDeviceQuality(bg(), "dev-none", day1)
	if err != nil || len(other) != 0 {
		t.Fatalf("unrelated device: %v %+v", err, other)
	}
}

// ADM-API-02e：窗口内质量统计（P50/P95/中继占比/采样数）。
func testQualityStatsSince(t *testing.T, s store.Store) {
	base := time.Date(2026, 11, 23, 6, 0, 0, 0, time.UTC)
	samples := []store.QualitySample{
		{SessionID: "ses-s1", TS: base, LatencyMs: 10},
		{SessionID: "ses-s1", TS: base.Add(time.Second), LatencyMs: 20},
		{SessionID: "ses-s1", TS: base.Add(2 * time.Second), LatencyMs: 30, Relay: true},
		{SessionID: "ses-s1", TS: base.Add(3 * time.Second), LatencyMs: 40, Relay: true},
	}
	if err := s.AppendQualitySamples(bg(), samples); err != nil {
		t.Fatalf("append: %v", err)
	}
	st, err := s.QualityStatsSince(bg(), base.Add(-time.Hour))
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if st.Samples != 4 {
		t.Fatalf("samples: %+v", st)
	}
	// 最近秩：P50 of [10,20,30,40] = 20；P95 = 40。
	if st.LatencyP50Ms != 20 || st.LatencyP95Ms != 40 || st.RelayPercent != 50 {
		t.Fatalf("percentiles: %+v", st)
	}
	// 窗口外为空（available 由 samples>0 判定）。
	empty, err := s.QualityStatsSince(bg(), base.Add(time.Hour))
	if err != nil || empty.Samples != 0 {
		t.Fatalf("empty window: %v %+v", err, empty)
	}
}

// ADM-API-04c：风险标记写入/解除，解除时清空原因。
func testSetDeviceRisk(t *testing.T, s store.Store) {
	d := &store.Device{DeviceCode: "960000001", AccountID: "acc-risk", Platform: "macOS"}
	if err := s.CreateDevice(bg(), d); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.SetDeviceRisk(bg(), d.ID, true, "异常连接"); err != nil {
		t.Fatalf("flag: %v", err)
	}
	got, err := s.GetDevice(bg(), d.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.RiskFlag || got.RiskReason != "异常连接" {
		t.Fatalf("flag not stored: %+v", got)
	}
	if err := s.SetDeviceRisk(bg(), d.ID, false, "误报"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got, _ = s.GetDevice(bg(), d.ID)
	if got.RiskFlag || got.RiskReason != "" {
		t.Fatalf("clear should wipe reason: %+v", got)
	}
	if err := s.SetDeviceRisk(bg(), "no-such", true, "x"); err == nil {
		t.Fatal("want ErrNotFound for unknown device")
	}
}

// ADM-API-06a：管理员列表/计数/改角色/启停。
func testAdminManagement(t *testing.T, s store.Store) {
	for _, u := range []*store.AdminUser{
		{Username: "root", PasswordHash: "x", TOTPSecret: "secret", Role: "super", Status: "active"},
		{Username: "ops1", PasswordHash: "x", Role: "ops", Status: "active"},
	} {
		if err := s.PutAdmin(bg(), u); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	list, err := s.ListAdmins(bg(), store.Page{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 || list[0].Username != "ops1" || list[1].Username != "root" {
		t.Fatalf("list order/contents: %+v", list)
	}
	if n, err := s.CountAdmins(bg()); err != nil || n != 2 {
		t.Fatalf("count: n=%d err=%v", n, err)
	}
	target := list[0]
	if err := s.UpdateAdminRole(bg(), target.ID, "cs"); err != nil {
		t.Fatalf("role: %v", err)
	}
	if err := s.UpdateAdminStatus(bg(), target.ID, "disabled"); err != nil {
		t.Fatalf("status: %v", err)
	}
	got, err := s.GetAdmin(bg(), target.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Role != "cs" || got.Status != "disabled" {
		t.Fatalf("update not applied: %+v", got)
	}
	if err := s.UpdateAdminRole(bg(), "no-such", "cs"); err == nil {
		t.Fatal("want ErrNotFound for unknown admin")
	}
}

// ADM-API-06b：版本按 platform+version 幂等覆盖，列表排序，下线状态。
func testClientVersions(t *testing.T, s store.Store) {
	v1 := &store.ClientVersion{Platform: "windows", Version: "0.2.13", Status: "rolling", RolloutPercent: 20, Notes: "灰度"}
	if err := s.UpsertVersion(bg(), v1); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	id := v1.ID
	// 同 platform+version 再写：必须更新同一行，ID 不变。
	v1b := &store.ClientVersion{Platform: "windows", Version: "0.2.13", Status: "full", RolloutPercent: 100}
	if err := s.UpsertVersion(bg(), v1b); err != nil {
		t.Fatalf("upsert2: %v", err)
	}
	if v1b.ID != id {
		t.Fatalf("upsert should keep id: %q vs %q", v1b.ID, id)
	}
	if err := s.UpsertVersion(bg(), &store.ClientVersion{Platform: "macos", Version: "0.2.12", Status: "draft"}); err != nil {
		t.Fatalf("upsert3: %v", err)
	}
	list, err := s.ListVersions(bg(), store.Page{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 || list[0].Platform != "macos" || list[1].Platform != "windows" {
		t.Fatalf("list: %+v", list)
	}
	if list[1].Status != "full" || list[1].RolloutPercent != 100 {
		t.Fatalf("upsert did not overwrite: %+v", list[1])
	}
	if n, err := s.CountVersions(bg()); err != nil || n != 2 {
		t.Fatalf("count: n=%d err=%v", n, err)
	}
	if err := s.UpdateVersionStatus(bg(), id, "retired"); err != nil {
		t.Fatalf("retire: %v", err)
	}
	got, _ := s.GetVersion(bg(), id)
	if got.Status != "retired" {
		t.Fatalf("status: %+v", got)
	}
	if err := s.UpdateVersionStatus(bg(), "no-such", "retired"); err == nil {
		t.Fatal("want ErrNotFound")
	}
}

// ADM-API-06c：黑名单（含 CIDR、过期）与告警。
func testBlacklistAndAlerts(t *testing.T, s store.Store) {
	now := time.Date(2026, 11, 23, 6, 0, 0, 0, time.UTC)
	exp := now.Add(-time.Hour)
	entries := []*store.BlacklistEntry{
		{Kind: "account", Value: "acc-bad", Note: "刷连接"},
		{Kind: "device", Value: "960000001", Note: "被盗"},
		{Kind: "ip", Value: "203.0.113.0/24", Note: "网段"},
		{Kind: "ip", Value: "198.51.100.7", Note: "单 IP", ExpiresAt: &exp},
	}
	for _, e := range entries {
		if err := s.AddBlacklist(bg(), e); err != nil {
			t.Fatalf("add: %v", err)
		}
	}
	// 重复加入同 kind+value 必须冲突。
	if err := s.AddBlacklist(bg(), &store.BlacklistEntry{Kind: "account", Value: "acc-bad"}); err != store.ErrConflict {
		t.Fatalf("want ErrConflict, got %v", err)
	}
	if n, err := s.CountBlacklist(bg()); err != nil || n != 4 {
		t.Fatalf("count: n=%d err=%v", n, err)
	}
	// CIDR 命中。
	if hit, err := s.MatchIPBlacklist(bg(), "203.0.113.42", now); err != nil || hit.Value != "203.0.113.0/24" {
		t.Fatalf("cidr match: %v %+v", err, hit)
	}
	// 过期条目不算命中。
	if _, err := s.MatchIPBlacklist(bg(), "198.51.100.7", now); err != store.ErrNotFound {
		t.Fatalf("expired entry should not match, got %v", err)
	}
	if _, err := s.FindActiveBlacklist(bg(), "account", "acc-bad", now); err != nil {
		t.Fatalf("find account: %v", err)
	}
	// 移除后查不到。
	list, err := s.ListBlacklist(bg(), store.Page{Limit: 10})
	if err != nil || len(list) != 4 {
		t.Fatalf("list: %v %+v", err, list)
	}
	if err := s.RemoveBlacklist(bg(), list[0].ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := s.GetBlacklist(bg(), list[0].ID); err != store.ErrNotFound {
		t.Fatalf("want not found after remove, got %v", err)
	}

	// 告警：记录 + 倒序列表。
	for _, sub := range []string{"account:acc-bad", "ip:203.0.113.42"} {
		if err := s.RecordAlert(bg(), &store.RiskAlert{Kind: "blacklist_block", Subject: sub, Detail: "test"}); err != nil {
			t.Fatalf("alert: %v", err)
		}
	}
	alerts, err := s.ListAlerts(bg(), store.Page{Limit: 10})
	if err != nil || len(alerts) != 2 {
		t.Fatalf("alerts: %v %+v", err, alerts)
	}
	if alerts[0].Subject != "ip:203.0.113.42" {
		t.Fatalf("alerts should be newest first: %+v", alerts)
	}
	if n, err := s.CountAlerts(bg()); err != nil || n != 2 {
		t.Fatalf("count alerts: n=%d err=%v", n, err)
	}
}
