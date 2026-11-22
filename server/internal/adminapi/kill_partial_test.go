package adminapi

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type failingCloser struct{}

func (failingCloser) NotifySessionClosed(_, _, _, _ string) error {
	return errors.New("bus down")
}

// ER-812：传播失败不能返回“完全成功”；必须给出 partial 结果，且审计记录实际结果。
func TestKillSessionReportsPartialPropagation(t *testing.T) {
	s, _, sess := setup(t)
	se, err := sess.Request(context.Background(), "c1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Consent(context.Background(), se.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	s.SetSessionCloser(failingCloser{})
	res, err := s.KillSession(context.Background(), Actor{Username: "ops", Role: RoleOps}, se.ID, "abuse")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Closed || res.Propagated {
		t.Fatalf("result=%+v, want closed && !propagated", res)
	}
	if !strings.Contains(res.Warning, "传播失败") {
		t.Fatalf("warning=%q", res.Warning)
	}
	if !res.Audited {
		t.Fatal("audit should be written with partial result")
	}
}
