package auth

import (
	"context"
	"testing"
	"time"

	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/store"
)

type fakeSMS struct{ code string }

func (f *fakeSMS) Send(phone, code string) error { return nil }
func (f *fakeSMS) Verify(phone, code string) error {
	if code == f.code {
		return nil
	}
	return errBad
}

type errStr string

func (e errStr) Error() string { return string(e) }

const errBad = errStr("bad code")

func newSvc(t *testing.T) (*Service, *store.Memory) {
	t.Helper()
	mem := store.NewMemory()
	s, err := New(mem, &fakeSMS{code: "123456"}, []byte("test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	return s, mem
}

func TestLoginCreatesAccountAndVerifiesToken(t *testing.T) {
	s, mem := newSvc(t)
	tok, acc, err := s.Login(ctx(), "13800008882", "123456")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if acc.ID == "" {
		t.Fatal("account id empty")
	}
	if countAccounts(mem) != 1 {
		t.Fatalf("want 1 account, got %d", countAccounts(mem))
	}
	got, err := s.VerifyToken(ctx(), tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.ID != acc.ID {
		t.Fatalf("account mismatch: %s vs %s", got.ID, acc.ID)
	}
}

func TestLoginSamePhoneReusesAccount(t *testing.T) {
	s, _ := newSvc(t)
	_, a1, _ := s.Login(ctx(), "13800008882", "123456")
	_, a2, _ := s.Login(ctx(), "13800008882", "123456")
	if a1.ID != a2.ID {
		t.Fatalf("want same account, got %s and %s", a1.ID, a2.ID)
	}
}

func TestLoginWrongCode(t *testing.T) {
	s, _ := newSvc(t)
	_, _, err := s.Login(ctx(), "13800008882", "000000")
	if !errcode.Is(err, errcode.AuthInvalidCredential) {
		t.Fatalf("want AUTH_INVALID_CREDENTIAL, got %v", err)
	}
}

func TestBannedAccountRejected(t *testing.T) {
	s, mem := newSvc(t)
	_, acc, _ := s.Login(ctx(), "13800008882", "123456")
	if err := mem.UpdateAccountStatus(ctx(), acc.ID, "banned"); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.Login(ctx(), "13800008882", "123456")
	if !errcode.Is(err, errcode.AuthAccountBanned) {
		t.Fatalf("want AUTH_ACCOUNT_BANNED, got %v", err)
	}
}

func TestTokenExpiry(t *testing.T) {
	s, _ := newSvc(t)
	now := time.Now().UTC()
	s.SetClock(func() time.Time { return now })
	tok, _, _ := s.Login(ctx(), "13800008882", "123456")

	s.SetClock(func() time.Time { return now.Add(31 * 24 * time.Hour) })
	if _, err := s.VerifyToken(ctx(), tok); !errcode.Is(err, errcode.AuthTokenExpired) {
		t.Fatalf("want expired, got %v", err)
	}
}

func TestTamperedTokenRejected(t *testing.T) {
	s, _ := newSvc(t)
	tok, _, _ := s.Login(ctx(), "13800008882", "123456")
	if _, err := s.VerifyToken(ctx(), tok+"x"); !errcode.Is(err, errcode.AuthTokenExpired) {
		t.Fatalf("want rejected, got %v", err)
	}
}

func TestEmptySecretRejected(t *testing.T) {
	if _, err := New(store.NewMemory(), &fakeSMS{}, nil); err == nil {
		t.Fatal("want error for empty secret")
	}
}

func ctx() context.Context { return context.Background() }

func countAccounts(m *store.Memory) int {
	list, err := m.ListAccounts(context.Background(), store.Page{Limit: 100})
	if err != nil {
		return -1
	}
	return len(list)
}

func TestSMSLoginStoresPhoneEnc(t *testing.T) {
	// 后台 phone_mask 依赖 PhoneEnc；短信注册建号必须写入归一化手机号。
	s, mem := newSvc(t)
	_, acc, err := s.Login(ctx(), "13800008882", "123456")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	stored, err := mem.GetAccount(ctx(), acc.ID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if stored.PhoneEnc != "13800008882" {
		t.Fatalf("want PhoneEnc stored, got %q", stored.PhoneEnc)
	}

	// 同一手机号再次登录复用账号，不能把 PhoneEnc 清空。
	_, again, err := s.Login(ctx(), "13800008882", "123456")
	if err != nil {
		t.Fatalf("relogin: %v", err)
	}
	if again.PhoneEnc != "13800008882" {
		t.Fatalf("relogin lost PhoneEnc: %q", again.PhoneEnc)
	}
}

// ADM-02b：历史账号只有 PhoneHash，登录时必须幂等回填 PhoneEnc。
func TestLoginBackfillsMissingPhoneEnc(t *testing.T) {
	s, mem := newSvc(t)
	// 模拟历史账号：只有哈希，没有密文。
	hash := s.phoneHash("13800008882")
	if err := mem.CreateAccount(ctx(), &store.Account{PhoneHash: hash, Status: "active"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, acc, err := s.Login(ctx(), "13800008882", "123456")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if acc.PhoneEnc != "13800008882" {
		t.Fatalf("login 返回值未回填: %q", acc.PhoneEnc)
	}
	stored, err := mem.GetAccount(ctx(), acc.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.PhoneEnc != "13800008882" {
		t.Fatalf("存储未回填: %q", stored.PhoneEnc)
	}

	// 幂等：再次登录不报错、值不变。
	if _, again, err := s.Login(ctx(), "13800008882", "123456"); err != nil || again.PhoneEnc != "13800008882" {
		t.Fatalf("relogin: %v %q", err, again.PhoneEnc)
	}
}

// ADM-API-03b：登录成功必须写入最近登录时间。
func TestLoginRecordsLastLoginAt(t *testing.T) {
	s, mem := newSvc(t)
	_, acc, err := s.Login(ctx(), "13800008882", "123456")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if acc.LastLoginAt.IsZero() {
		t.Fatal("login 返回值缺 LastLoginAt")
	}
	stored, err := mem.GetAccount(ctx(), acc.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.LastLoginAt.IsZero() {
		t.Fatal("存储未写入 LastLoginAt")
	}
}
