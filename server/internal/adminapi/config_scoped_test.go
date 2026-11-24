package adminapi

import (
	"context"
	"testing"

	"github.com/easyremote/server/internal/errcode"
)

// ADM-API-05c1：作用域写入审计带作用域，平台值不影响全局。
func TestScopedConfigAudit(t *testing.T) {
	s, mem, _ := setup(t)
	ctxb := context.Background()
	super := Actor{Username: "root", Role: RoleSuper}

	if _, err := s.PutConfigFor(ctxb, super, "", map[string]string{"session.max_minutes": "600"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutConfigFor(ctxb, super, "windows", map[string]string{"session.max_minutes": "300"}); err != nil {
		t.Fatal(err)
	}
	win, err := s.GetConfigFor(ctxb, super, "windows")
	if err != nil || win["session.max_minutes"] != "300" {
		t.Fatalf("windows: %v %+v", err, win)
	}
	global, err := s.GetConfigFor(ctxb, super, "")
	if err != nil || global["session.max_minutes"] != "600" {
		t.Fatalf("global: %v %+v", err, global)
	}
	// 审计对象要能区分作用域。
	var scopedAudit bool
	for _, a := range listAudit(mem) {
		if a.Action == "config.update" && a.Object == "app_config@windows" && a.Result == "ok" {
			scopedAudit = true
		}
	}
	if !scopedAudit {
		t.Fatal("缺 app_config@windows 审计")
	}
	// 非法平台。
	if _, err := s.PutConfigFor(ctxb, super, "linux", map[string]string{"session.max_minutes": "10"}); !errcode.Is(err, errcode.AdminConfigInvalid) {
		t.Fatalf("want invalid platform, got %v", err)
	}
}
