package store

import (
	"context"
	"sync"
	"testing"
	"time"
)

func ctx() context.Context { return context.Background() }

func TestRandomIDsAreNotEnumerable(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		id := randomID("acc")
		if seen[id] {
			t.Fatalf("duplicate id %s", id)
		}
		seen[id] = true
	}
}

func TestNotFoundIsDistinct(t *testing.T) {
	m := NewMemory()
	if _, err := m.GetAccount(ctx(), "nope"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestAccountConflict(t *testing.T) {
	m := NewMemory()
	a := &Account{PhoneHash: "h1", Status: "active"}
	if err := m.CreateAccount(ctx(), a); err != nil {
		t.Fatal(err)
	}
	if err := m.CreateAccount(ctx(), &Account{PhoneHash: "h1"}); err != ErrConflict {
		t.Fatalf("want conflict, got %v", err)
	}
}

func TestPagination(t *testing.T) {
	m := NewMemory()
	for i := 0; i < 5; i++ {
		_ = m.CreateAccount(ctx(), &Account{PhoneHash: string(rune('a' + i)), Status: "active"})
	}
	page, err := m.ListAccounts(ctx(), Page{Limit: 2, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 {
		t.Fatalf("want 2, got %d", len(page))
	}
}

func TestVerifyAndConsumeIsAtomicUnderConcurrency(t *testing.T) {
	m := NewMemory()
	now := time.Now().UTC()
	_ = m.UpsertVerifyCode(ctx(), &VerifyCode{DeviceCode: "dev1", CodeHash: "hash", ExpiresAt: now.Add(time.Minute)})

	const n = 50
	var wg sync.WaitGroup
	var mu sync.Mutex
	invalid, locked := 0, 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := m.VerifyAndConsume(ctx(), "dev1", "wrong", now, 5, time.Minute)
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			switch res.Outcome {
			case VerifyInvalid:
				invalid++
			case VerifyLocked:
				locked++
			}
		}()
	}
	wg.Wait()
	if invalid != 4 {
		t.Fatalf("want exactly 4 invalid before lock, got %d (locked=%d)", invalid, locked)
	}
	if locked != n-4 {
		t.Fatalf("want %d locked, got %d", n-4, locked)
	}
}

func TestVerifySingleUseAndExpiry(t *testing.T) {
	m := NewMemory()
	now := time.Now().UTC()
	_ = m.UpsertVerifyCode(ctx(), &VerifyCode{DeviceCode: "d", CodeHash: "h", ExpiresAt: now.Add(time.Minute)})
	if res, _ := m.VerifyAndConsume(ctx(), "d", "h", now, 5, time.Minute); res.Outcome != VerifyOK {
		t.Fatalf("first use: %v", res.Outcome)
	}
	if res, _ := m.VerifyAndConsume(ctx(), "d", "h", now, 5, time.Minute); res.Outcome == VerifyOK {
		t.Fatal("replay must be rejected")
	}
	if res, _ := m.VerifyAndConsume(ctx(), "missing", "h", now, 5, time.Minute); res.Outcome != VerifyExpired {
		t.Fatalf("missing should be expired, got %v", res.Outcome)
	}
}

func TestPutAdminUpsertByUsername(t *testing.T) {
	m := NewMemory()
	_ = m.PutAdmin(ctx(), &AdminUser{Username: "root", PasswordHash: "a", Role: "super"})
	_ = m.PutAdmin(ctx(), &AdminUser{Username: "root", PasswordHash: "b", Role: "ops"})
	got, err := m.GetAdminByUsername(ctx(), "root")
	if err != nil {
		t.Fatal(err)
	}
	if got.PasswordHash != "b" || got.Role != "ops" {
		t.Fatalf("upsert failed: %+v", got)
	}
}
