package adminapi

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/password"
	"github.com/easyremote/server/internal/session"
	"github.com/easyremote/server/internal/store"
)

func setup(t *testing.T) (*Service, *store.Memory, *session.Service) {
	t.Helper()
	mem := store.NewMemory()
	_ = mem.CreateAccount(context.Background(), &store.Account{ID: "acc1", PhoneHash: "h1", PhoneEnc: "13800008882", Status: "active"})
	_ = mem.CreateAccount(context.Background(), &store.Account{ID: "acc2", PhoneHash: "h2", PhoneEnc: "13900001111", Status: "active"})
	sess := session.New(mem)
	return New(mem, sess), mem, sess
}

func TestPermissionMatrix(t *testing.T) {
	cases := []struct {
		role, action string
		want         bool
	}{
		{RoleSuper, "session.kill", true},
		{RoleReadonly, "session.kill", false},
		{RoleRisk, "user.ban", true},
		{RoleCS, "user.ban", false},
		{RoleCS, "session.kill", true},
		{RoleReadonly, "audit.read", true},
		{RoleOps, "unknown.action", false},
	}
	for _, c := range cases {
		if got := Can(c.role, c.action); got != c.want {
			t.Fatalf("Can(%s,%s)=%v want %v", c.role, c.action, got, c.want)
		}
	}
}

func TestBanRequiresReason(t *testing.T) {
	s, _, _ := setup(t)
	act := Actor{Username: "root", Role: RoleSuper, IP: "10.0.0.1"}
	if err := s.BanUser(ctx(), act, "acc1", "  "); !errcode.Is(err, errcode.AdminReasonRequired) {
		t.Fatalf("want reason required, got %v", err)
	}
}

func TestBanWritesAudit(t *testing.T) {
	s, mem, _ := setup(t)
	act := Actor{Username: "root", Role: RoleSuper, IP: "10.0.0.1"}
	if err := s.BanUser(ctx(), act, "acc1", "abuse"); err != nil {
		t.Fatal(err)
	}
	logs := listAudit(mem)
	if len(logs) != 1 {
		t.Fatalf("want 1 audit, got %d", len(logs))
	}
	if logs[0].Action != "user.ban" || logs[0].Result != "ok" {
		t.Fatalf("unexpected audit: %+v", logs[0])
	}
	acc, _ := mem.GetAccount(ctx(), "acc1")
	if acc.Status != "banned" {
		t.Fatalf("want banned, got %s", acc.Status)
	}
}

func TestForbiddenActionStillAudited(t *testing.T) {
	s, mem, _ := setup(t)
	act := Actor{Username: "viewer", Role: RoleReadonly, IP: "10.0.0.9"}
	if err := s.BanUser(ctx(), act, "acc1", "x"); !errcode.Is(err, errcode.AdminForbidden) {
		t.Fatalf("want forbidden, got %v", err)
	}
	logs := listAudit(mem)
	if len(logs) != 1 || logs[0].Result != "denied" {
		t.Fatalf("want denied audit, got %+v", logs)
	}
}

func TestKillSessionRequiresReasonAndAudits(t *testing.T) {
	s, mem, sess := setup(t)
	act := Actor{Username: "cs", Role: RoleCS, IP: "10.0.0.2"}
	se, _ := sess.Request(ctx(), "c1", "t1")
	_, _ = sess.Consent(ctx(), se.ID, true, "")

	if _, err := s.KillSession(ctx(), act, se.ID, ""); !errcode.Is(err, errcode.AdminReasonRequired) {
		t.Fatalf("want reason required, got %v", err)
	}
	if _, err := s.KillSession(ctx(), act, se.ID, "user report"); err != nil {
		t.Fatal(err)
	}
	got, _ := sess.Get(ctx(), se.ID)
	if got.State != session.StateClosed {
		t.Fatalf("want closed, got %s", got.State)
	}
	if len(listAudit(mem)) != 2 {
		t.Fatalf("want 2 audits, got %d", len(listAudit(mem)))
	}
}

func TestUserListMasksPhone(t *testing.T) {
	s, _, _ := setup(t)
	act := Actor{Username: "ops", Role: RoleOps}
	list, err := s.ListUsers(ctx(), act, store.Page{Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("want users")
	}
	for _, u := range list {
		if u.PhoneMask == "13800008882" {
			t.Fatal("phone not masked")
		}
	}
}

func TestPasswordHashingIsSalted(t *testing.T) {
	// argon2id 每次哈希带随机 salt，两次结果必然不同——这是正确行为
	a, b := HashPassword("dev-pass"), HashPassword("dev-pass")
	if a == b {
		t.Fatal("hashes must differ (per-user salt)")
	}
	if !password.IsEncoded(a) {
		t.Fatalf("unexpected format: %s", a)
	}
	if err := password.Verify("dev-pass", a); err != nil {
		t.Fatalf("verify failed: %v", err)
	}
}

func TestLegacyHashStillVerifies(t *testing.T) {
	legacy := legacyHash("old-pass")
	if !checkPassword("old-pass", legacy) {
		t.Fatal("legacy hash should still verify")
	}
	if checkPassword("wrong", legacy) {
		t.Fatal("legacy hash must reject wrong password")
	}
}

func TestAuthenticateWrongPassword(t *testing.T) {
	s, mem, _ := setup(t)
	mem.PutAdmin(context.Background(), &store.AdminUser{
		Username: "root", PasswordHash: HashPassword("right"),
		TOTPSecret: "secret", Role: RoleSuper, Status: "active",
	})
	if _, err := s.Authenticate(ctx(), "root", "wrong", "000000"); !errcode.Is(err, errcode.AuthInvalidCredential) {
		t.Fatalf("want invalid credential, got %v", err)
	}
}

func TestAuthenticateRequiresMFA(t *testing.T) {
	s, mem, _ := setup(t)
	mem.PutAdmin(context.Background(), &store.AdminUser{
		Username: "root", PasswordHash: HashPassword("right"), Role: RoleSuper, Status: "active",
	})
	// 未配置 TOTP 的账号不得登录（强制 MFA）
	if _, err := s.Authenticate(ctx(), "root", "right", ""); !errcode.Is(err, errcode.AdminMfaRequired) {
		t.Fatalf("want ADMIN_MFA_REQUIRED, got %v", err)
	}
}

func TestAuthenticateRejectsDisabledAccount(t *testing.T) {
	s, mem, _ := setup(t)
	mem.PutAdmin(context.Background(), &store.AdminUser{
		Username: "root", PasswordHash: HashPassword("right"),
		TOTPSecret: "s", Role: RoleSuper, Status: "disabled",
	})
	if _, err := s.Authenticate(ctx(), "root", "right", "000000"); !errcode.Is(err, errcode.AuthAccountBanned) {
		t.Fatalf("want AUTH_ACCOUNT_BANNED, got %v", err)
	}
}

func TestLoginAuditWritten(t *testing.T) {
	s, mem, _ := setup(t)
	s.AuditLogin(ctx(), "root", "10.0.0.1", false)
	logs, err := mem.ListAudit(ctx(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].Action != "admin.login" || logs[0].Result != "denied" {
		t.Fatalf("unexpected audit: %+v", logs)
	}
}

func ctx() context.Context { return context.Background() }

func listAudit(m *store.Memory) []*store.AuditLog {
	logs, err := m.ListAudit(context.Background(), 10)
	if err != nil {
		return nil
	}
	return logs
}

// TestListUsers_Mask：后台列表的手机号必须脱敏成 138****8888。
// 历史缺陷：短信注册账号没写 PhoneEnc，maskPhone("") 返回 "***"，而旧断言只检查
// "不等于明文"，所以一直没红（ADM-02a）。
func TestListUsers_Mask(t *testing.T) {
	s, _, _ := setup(t)
	act := Actor{Username: "ops", Role: RoleOps}
	list, err := s.ListUsers(ctx(), act, store.Page{Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("want users")
	}
	maskRe := regexp.MustCompile("^[0-9]{3}\\*{4}[0-9]{4}$")
	for _, u := range list {
		if !maskRe.MatchString(u.PhoneMask) {
			t.Fatalf("phone_mask %q 不符合脱敏格式（账号 %s）", u.PhoneMask, u.ID)
		}
	}
}

// TestListUsers_Empty：空库必须返回空数组语义（len==0），而不是 nil 指针或错误。
func TestListUsers_Empty(t *testing.T) {
	mem := store.NewMemory()
	s := New(mem, session.New(mem))
	act := Actor{Username: "ops", Role: RoleOps}
	list, err := s.ListUsers(ctx(), act, store.Page{Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	if list == nil {
		t.Fatal("空库应返回空切片，便于 JSON 序列化成 []")
	}
	if len(list) != 0 {
		t.Fatalf("want 0 users, got %d", len(list))
	}
}

// TestListUsers_Forbidden：没有 user.read 的角色必须拿到 ADMIN_FORBIDDEN。
// 注意 readonly 在现行权限表里**有** user.read（permission map），所以用未知角色验证拒绝路径。
func TestListUsers_Forbidden(t *testing.T) {
	s, _, _ := setup(t)
	act := Actor{Username: "guest", Role: "guest"}
	_, err := s.ListUsers(ctx(), act, store.Page{Limit: 200})
	if err == nil {
		t.Fatal("want forbidden")
	}
	if !errcode.Is(err, errcode.AdminForbidden) {
		t.Fatalf("want ADMIN_FORBIDDEN, got %v", err)
	}
}

// ADM-API-02a：在线用户按账号去重（同一账号两台在线设备只算一个用户）。
func TestOverviewUsersOnlineCountsDistinctAccounts(t *testing.T) {
	mem := store.NewMemory()
	ctxb := context.Background()
	now := time.Now().UTC()
	_ = mem.CreateAccount(ctxb, &store.Account{ID: "acc-a", PhoneHash: "ha", PhoneEnc: "13800000001", Status: "active"})
	_ = mem.CreateAccount(ctxb, &store.Account{ID: "acc-b", PhoneHash: "hb", PhoneEnc: "13800000002", Status: "active"})
	for _, d := range []*store.Device{
		{ID: "d1", DeviceCode: "111111111", AccountID: "acc-a", Platform: "macOS", Online: true, LastSeenAt: now},
		{ID: "d2", DeviceCode: "222222222", AccountID: "acc-a", Platform: "Windows", Online: true, LastSeenAt: now},
		{ID: "d3", DeviceCode: "333333333", AccountID: "acc-b", Platform: "Android", Online: false, LastSeenAt: now},
	} {
		if err := mem.CreateDevice(ctxb, d); err != nil {
			t.Fatal(err)
		}
	}
	s := New(mem, session.New(mem))
	ov, err := s.Overview(ctxb, Actor{Username: "ops", Role: RoleOps})
	if err != nil {
		t.Fatal(err)
	}
	if ov.DevicesTotal != 3 || ov.DevicesOnline != 2 {
		t.Fatalf("devices: total=%d online=%d", ov.DevicesTotal, ov.DevicesOnline)
	}
	if ov.UsersOnline != 1 {
		t.Fatalf("users_online 必须按账号去重，want 1 got %d", ov.UsersOnline)
	}
}

// ADM-API-03a：用户列表返回设备数（一次聚合，不是前端数）。
func TestListUsers_DeviceCount(t *testing.T) {
	s, mem, _ := setup(t)
	ctxb := context.Background()
	for _, d := range []*store.Device{
		{ID: "d1", DeviceCode: "700000001", AccountID: "acc1", Platform: "macOS"},
		{ID: "d2", DeviceCode: "700000002", AccountID: "acc1", Platform: "Windows"},
	} {
		if err := mem.CreateDevice(ctxb, d); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.ListUsers(ctxb, Actor{Username: "ops", Role: RoleOps}, store.Page{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]int{}
	for _, u := range list {
		byID[u.ID] = u.DeviceCount
	}
	if byID["acc1"] != 2 || byID["acc2"] != 0 {
		t.Fatalf("device_count 不符: %+v", byID)
	}
}

// ADM-API-04b：强制下线关闭该设备的会话并标记离线；解绑删除设备；两者都要求原因并写审计。
func TestForceOfflineClosesSessionsAndMarksOffline(t *testing.T) {
	s, mem, sess := setup(t)
	ctxb := context.Background()
	d := &store.Device{ID: "dev-fo", DeviceCode: "930000001", AccountID: "acc1", Platform: "macOS", Online: true}
	if err := mem.CreateDevice(ctxb, d); err != nil {
		t.Fatal(err)
	}
	other := &store.Device{ID: "dev-peer", DeviceCode: "930000002", AccountID: "acc1", Platform: "Windows", Online: true}
	if err := mem.CreateDevice(ctxb, other); err != nil {
		t.Fatal(err)
	}
	se, err := sess.Request(ctxb, d.ID, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ForceOffline(ctxb, Actor{Username: "cs", Role: RoleCS}, d.ID, "用户报障"); err != nil {
		t.Fatal(err)
	}
	got, _ := sess.Get(ctxb, se.ID)
	if got.State != session.StateClosed {
		t.Fatalf("want closed, got %s", got.State)
	}
	dev, _ := mem.GetDevice(ctxb, d.ID)
	if dev.Online {
		t.Fatal("device 应被标记为离线")
	}
	if len(listAudit(mem)) != 1 || listAudit(mem)[0].Action != "device.force_offline" {
		t.Fatalf("audit: %+v", listAudit(mem))
	}
}

func TestForceOfflineRequiresReasonAndPermission(t *testing.T) {
	s, mem, _ := setup(t)
	ctxb := context.Background()
	d := &store.Device{ID: "dev-fo2", DeviceCode: "930000003", AccountID: "acc1", Platform: "macOS", Online: true}
	if err := mem.CreateDevice(ctxb, d); err != nil {
		t.Fatal(err)
	}
	if err := s.ForceOffline(ctxb, Actor{Username: "cs", Role: RoleCS}, d.ID, "  "); !errcode.Is(err, errcode.AdminReasonRequired) {
		t.Fatalf("want reason required, got %v", err)
	}
	if err := s.ForceOffline(ctxb, Actor{Username: "ro", Role: RoleReadonly}, d.ID, "x"); !errcode.Is(err, errcode.AdminForbidden) {
		t.Fatalf("want forbidden, got %v", err)
	}
	if err := s.ForceOffline(ctxb, Actor{Username: "cs", Role: RoleCS}, "no-such", "x"); !errcode.Is(err, errcode.ConnDeviceNotFound) {
		t.Fatalf("want device not found, got %v", err)
	}
}

func TestUnbindDeviceDeletesAndAudits(t *testing.T) {
	s, mem, _ := setup(t)
	ctxb := context.Background()
	d := &store.Device{ID: "dev-unbind", DeviceCode: "930000004", AccountID: "acc1", Platform: "macOS"}
	if err := mem.CreateDevice(ctxb, d); err != nil {
		t.Fatal(err)
	}
	// 解绑只给超级管理员。
	if err := s.UnbindDevice(ctxb, Actor{Username: "ops", Role: RoleOps}, d.ID, "x"); !errcode.Is(err, errcode.AdminForbidden) {
		t.Fatalf("want forbidden for ops, got %v", err)
	}
	if err := s.UnbindDevice(ctxb, Actor{Username: "root", Role: RoleSuper}, d.ID, "设备丢失"); err != nil {
		t.Fatal(err)
	}
	if _, err := mem.GetDevice(ctxb, d.ID); err == nil {
		t.Fatal("设备应已删除")
	}
	// 被拒绝的那次也要留痕（denied），成功的那次记 ok——两次都要有。
	audits := listAudit(mem)
	var denied, ok int
	for _, a := range audits {
		if a.Action != "device.unbind" {
			t.Fatalf("unexpected action: %+v", a)
		}
		switch a.Result {
		case "denied":
			denied++
		case "ok":
			ok++
		}
	}
	if denied != 1 || ok != 1 {
		t.Fatalf("want 1 denied + 1 ok, got %d/%d (%+v)", denied, ok, audits)
	}
}

// ADM-API-05a：审计合法动作集合来自服务端，不从前端当前页猜。
func TestAuditActionsReturnsServerSet(t *testing.T) {
	s, mem, _ := setup(t)
	ctxb := context.Background()
	for _, a := range []*store.AuditLog{
		{Actor: "root", Role: "super", Action: "user.ban", Object: "acc1", Result: "ok", IP: "127.0.0.1", TS: time.Now().UTC()},
		{Actor: "root", Role: "super", Action: "config.update", Object: "session.max_minutes", Result: "ok", IP: "127.0.0.1", TS: time.Now().UTC()},
	} {
		if err := mem.AppendAudit(ctxb, a); err != nil {
			t.Fatal(err)
		}
	}
	res, err := s.AuditActions(ctxb, Actor{Username: "root", Role: RoleSuper})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Actions) != 2 || res.Actions[0] != "config.update" || res.Actions[1] != "user.ban" {
		t.Fatalf("actions: %+v", res.Actions)
	}
	// readonly 有 audit.read，可以看；未知角色必须被拒。
	if _, err := s.AuditActions(ctxb, Actor{Username: "ro", Role: RoleReadonly}); err != nil {
		t.Fatalf("readonly should read actions: %v", err)
	}
	if _, err := s.AuditActions(ctxb, Actor{Username: "guest", Role: "guest"}); !errcode.Is(err, errcode.AdminForbidden) {
		t.Fatalf("want forbidden, got %v", err)
	}
}
