package appconfig

import (
	"context"
	"testing"

	"github.com/easyremote/server/internal/store"
)

// ADM-API-05c1：平台覆盖优先于全局覆盖，全局改动不影响平台作用域里的键。
func TestScopedConfigPrecedence(t *testing.T) {
	mem := store.NewMemory()
	s := New(mem)
	ctx := context.Background()

	// 初始：全部默认值。
	all, err := s.AllFor(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if all[KeySessionMaxMinutes] != "720" {
		t.Fatalf("default: %q", all[KeySessionMaxMinutes])
	}

	// 全局改成 600。
	if _, err := s.UpdateFor(ctx, "", map[string]string{KeySessionMaxMinutes: "600"}); err != nil {
		t.Fatal(err)
	}
	// 平台 windows 再改成 300。
	if _, err := s.UpdateFor(ctx, "windows", map[string]string{KeySessionMaxMinutes: "300"}); err != nil {
		t.Fatal(err)
	}

	global, _ := s.AllFor(ctx, "")
	windows, _ := s.AllFor(ctx, "windows")
	macos, _ := s.AllFor(ctx, "macos")
	if global[KeySessionMaxMinutes] != "600" {
		t.Fatalf("global: %q", global[KeySessionMaxMinutes])
	}
	if windows[KeySessionMaxMinutes] != "300" {
		t.Fatalf("windows: %q", windows[KeySessionMaxMinutes])
	}
	// 未单独覆盖的平台继承全局值。
	if macos[KeySessionMaxMinutes] != "600" {
		t.Fatalf("macos should inherit global: %q", macos[KeySessionMaxMinutes])
	}

	// 版本作用域优先于平台作用域（ADM-API-05d）。
	if _, err := s.UpdateForScope(ctx, "windows", "0.2.13", map[string]string{KeySessionMaxMinutes: "120"}); err != nil {
		t.Fatal(err)
	}
	verScoped, _ := s.AllForScope(ctx, "windows", "0.2.13")
	otherVer, _ := s.AllForScope(ctx, "windows", "0.2.12")
	if verScoped[KeySessionMaxMinutes] != "120" {
		t.Fatalf("version scope: %q", verScoped[KeySessionMaxMinutes])
	}
	// 同平台的其它版本回落到平台作用域。
	if otherVer[KeySessionMaxMinutes] != "300" {
		t.Fatalf("other version should fall back to platform: %q", otherVer[KeySessionMaxMinutes])
	}
	// 版本作用域必须带平台。
	if _, err := s.UpdateForScope(ctx, "", "0.2.13", map[string]string{KeySessionMaxMinutes: "1"}); err == nil {
		t.Fatal("version scope without platform must fail")
	}

	// 非法平台必须被拒。
	if _, err := s.AllFor(ctx, "linux"); err == nil {
		t.Fatal("want error for unknown platform")
	}
	if _, err := s.UpdateFor(ctx, "linux", map[string]string{KeySessionMaxMinutes: "1"}); err == nil {
		t.Fatal("want error for unknown platform on update")
	}
	// 作用域写入也要走校验。
	if _, err := s.UpdateFor(ctx, "windows", map[string]string{KeySessionMaxMinutes: "9999"}); err == nil {
		t.Fatal("want range validation")
	}
}
