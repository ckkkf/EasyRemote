package adminapi

import (
	"context"
	"testing"
	"time"

	"github.com/easyremote/server/internal/session"
	"github.com/easyremote/server/internal/store"
)

// ADM-API-03f2a：关闭时落 close_kind；按异常结束/链路类型筛选。
func TestSessionCloseKindAndFilters(t *testing.T) {
	s, mem, sess := setup(t)
	ctxb := context.Background()
	act := Actor{Username: "ops", Role: RoleOps}
	for _, d := range []*store.Device{
		{ID: "ka1", DeviceCode: "996000001", AccountID: "accK", Platform: "macOS"},
		{ID: "ka2", DeviceCode: "996000002", AccountID: "accK", Platform: "Windows"},
	} {
		if err := mem.CreateDevice(ctxb, d); err != nil {
			t.Fatal(err)
		}
	}
	// 一条用户挂断（正常），一条网络中断（异常）。
	normal, err := sess.Request(ctxb, "ka1", "ka2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Kill(ctxb, normal.ID, "user hangup"); err != nil {
		t.Fatal(err)
	}
	abnormal, err := sess.Request(ctxb, "ka1", "ka2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Kill(ctxb, abnormal.ID, "network changed"); err != nil {
		t.Fatal(err)
	}

	closed, err := mem.GetSession(ctxb, normal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if closed.CloseKind != string(session.CloseUserHangup) {
		t.Fatalf("close kind: %q", closed.CloseKind)
	}

	// 异常结束筛选。
	yes := true
	bad, err := s.ListSessions(ctxb, act, "", store.Page{Limit: 10, Abnormal: &yes})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range bad {
		if r.ID == normal.ID {
			t.Fatalf("normal session should be excluded: %+v", bad)
		}
	}
	found := false
	for _, r := range bad {
		if r.ID == abnormal.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("abnormal session missing from filter result")
	}

	// 正常结束筛选必须包含刚关闭的 normal 会话。
	no := false
	ok, err := s.ListSessions(ctxb, act, "", store.Page{Limit: 10, Abnormal: &no})
	if err != nil {
		t.Fatal(err)
	}
	hasNormal := false
	for _, r := range ok {
		if r.ID == normal.ID {
			hasNormal = true
		}
	}
	if !hasNormal {
		t.Fatalf("normal filter missing normal session: %+v", ok)
	}

	// 链路筛选：给 abnormal 会话写 relay 采样。
	now := time.Now().UTC().Truncate(time.Second)
	if err := mem.AppendQualitySamples(ctxb, []store.QualitySample{
		{SessionID: abnormal.ID, TS: now, LatencyMs: 30, Link: "relay"},
		{SessionID: abnormal.ID, TS: now.Add(-time.Second), LatencyMs: 32, Link: "relay"},
	}); err != nil {
		t.Fatal(err)
	}
	relayRows, err := s.ListSessions(ctxb, act, "", store.Page{Limit: 10, Link: "relay"})
	if err != nil {
		t.Fatal(err)
	}
	if len(relayRows) != 1 || relayRows[0].ID != abnormal.ID {
		t.Fatalf("link filter: %+v", relayRows)
	}
}
