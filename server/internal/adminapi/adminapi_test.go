package adminapi

import (
	"context"
	"testing"

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

	if err := s.KillSession(ctx(), act, se.ID, ""); !errcode.Is(err, errcode.AdminReasonRequired) {
		t.Fatalf("want reason required, got %v", err)
	}
	if err := s.KillSession(ctx(), act, se.ID, "user report"); err != nil {
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
	list, err := s.ListUsers(ctx(), act)
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
