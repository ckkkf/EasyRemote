package store

import (
	"context"
	"testing"
	"time"
)

func TestPageQuerySortMemory(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	now := time.Now().UTC()
	for _, a := range []*Account{
		{ID: "a1", PhoneHash: "h1", PhoneEnc: "13800000001", Status: "active", CreatedAt: now},
		{ID: "a2", PhoneHash: "h2", PhoneEnc: "13900000002", Status: "banned", CreatedAt: now.Add(time.Second)},
	} {
		if err := m.CreateAccount(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	got, err := m.ListAccounts(ctx, Page{Query: "139", Sort: "id", Desc: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "a2" {
		t.Fatalf("query result=%+v", got)
	}
	got, _ = m.ListAccounts(ctx, Page{Status: "banned"})
	if len(got) != 1 || got[0].ID != "a2" {
		t.Fatalf("status result=%+v", got)
	}

	if err := m.CreateDevice(ctx, &Device{ID: "d1", DeviceCode: "111111111", AccountID: "a1", Platform: "macos"}); err != nil {
		t.Fatal(err)
	}
	if err := m.CreateDevice(ctx, &Device{ID: "d2", DeviceCode: "222222222", AccountID: "a2", Platform: "windows"}); err != nil {
		t.Fatal(err)
	}
	devs, _ := m.ListDevices(ctx, Page{Query: "windows"})
	if len(devs) != 1 || devs[0].ID != "d2" {
		t.Fatalf("device query=%+v", devs)
	}
	devs, _ = m.ListDevices(ctx, Page{Sort: "platform", Desc: true})
	if len(devs) != 2 || devs[0].Platform != "windows" {
		t.Fatalf("device sort=%+v", devs)
	}

	se := &Session{ID: "s1", ControllerID: "c1", TargetID: "t1", State: "active", StartedAt: now}
	if err := m.CreateSession(ctx, se); err != nil {
		t.Fatal(err)
	}
	ss, _ := m.ListSessions(ctx, "", Page{Query: "c1"})
	if len(ss) != 1 {
		t.Fatalf("session query=%+v", ss)
	}
}
func TestPageCountsMatchFilters(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	for i, a := range []*Account{
		{ID: "a1", PhoneHash: "h1", PhoneEnc: "13800000001", Status: "active"},
		{ID: "a2", PhoneHash: "h2", PhoneEnc: "13900000002", Status: "banned"},
		{ID: "a3", PhoneHash: "h3", PhoneEnc: "13700000003", Status: "active"},
	} {
		a.CreatedAt = time.Now().Add(time.Duration(i) * time.Second)
		if err := m.CreateAccount(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := m.CountAccounts(ctx, Page{Status: "active"}); n != 2 {
		t.Fatalf("active count=%d", n)
	}
	if n, _ := m.CountAccounts(ctx, Page{Query: "139"}); n != 1 {
		t.Fatalf("query count=%d", n)
	}
}
func TestPagePlatformAndAuditFilters(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	_ = m.CreateDevice(ctx, &Device{ID: "d1", DeviceCode: "111111111", AccountID: "a1", Platform: "macos"})
	_ = m.CreateDevice(ctx, &Device{ID: "d2", DeviceCode: "222222222", AccountID: "a2", Platform: "windows"})
	devs, _ := m.ListDevices(ctx, Page{Platform: "windows"})
	if len(devs) != 1 || devs[0].ID != "d2" {
		t.Fatalf("platform filter=%+v", devs)
	}
	_ = m.AppendAudit(ctx, &AuditLog{ID: "au1", Actor: "root", Action: "user.ban", Result: "ok"})
	_ = m.AppendAudit(ctx, &AuditLog{ID: "au2", Actor: "root", Action: "session.kill", Result: "denied"})
	logs, _ := m.ListAuditPage(ctx, Page{Action: "session.kill"})
	if len(logs) != 1 || logs[0].ID != "au2" {
		t.Fatalf("action filter=%+v", logs)
	}
	logs, _ = m.ListAuditPage(ctx, Page{Result: "ok"})
	if len(logs) != 1 || logs[0].ID != "au1" {
		t.Fatalf("result filter=%+v", logs)
	}
}
