package adminapi

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/store"
)

// ADM-API-05c2：导出 CSV 的脱敏、转义、截断标记与导出审计。
func TestExportAuditCSV(t *testing.T) {
	s, mem, _ := setup(t)
	ctxb := context.Background()
	now := time.Now().UTC()
	entries := []*store.AuditLog{
		{Actor: "root", Role: "super", Action: "user.ban", Object: "acc1", Result: "ok", IP: "203.0.113.42", TS: now},
		{Actor: "root", Role: "super", Action: "config.update", Object: "k,with,comma", Before: `{"a":"1"}`, After: `{"a":"2"}`, Result: "ok", IP: "2001:db8::1", TS: now},
	}
	for _, a := range entries {
		if err := mem.AppendAudit(ctxb, a); err != nil {
			t.Fatal(err)
		}
	}
	res, err := s.ExportAudit(ctxb, Actor{Username: "root", Role: RoleSuper}, store.Page{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if res.Rows != 2 || res.Truncated {
		t.Fatalf("rows: %+v", res)
	}
	lines := strings.Split(strings.TrimRight(res.Csv, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want header + 2 rows, got %d", len(lines))
	}
	if !strings.HasPrefix(lines[0], "时间,操作人,角色,动作") {
		t.Fatalf("header: %q", lines[0])
	}
	// IP 脱敏：IPv4 保留前两段，IPv6 保留首段。
	if !strings.Contains(res.Csv, "203.0.x.x") || !strings.Contains(res.Csv, "2001:****") {
		t.Fatalf("ip not masked: %q", res.Csv)
	}
	// IPv4:port（RemoteAddr 原样）也必须按 IPv4 脱敏，而不是当成 IPv6。
	if got := maskIP("127.0.0.1:54321"); got != "127.0.x.x:54321" {
		t.Fatalf("mask ipv4:port = %q", got)
	}
	// 逗号字段必须被引号包裹。
	if !strings.Contains(res.Csv, "\"k,with,comma\"") {
		t.Fatalf("comma not escaped: %q", res.Csv)
	}
	// 导出动作本身进审计。
	var exportAudit bool
	for _, a := range listAudit(mem) {
		if a.Action == "audit.export" && a.Result == "ok" {
			exportAudit = true
		}
	}
	if !exportAudit {
		t.Fatal("缺 audit.export 审计")
	}
	// 截断：limit 小于总数时 truncated=true。
	trunc, err := s.ExportAudit(ctxb, Actor{Username: "root", Role: RoleSuper}, store.Page{}, 1)
	if err != nil || !trunc.Truncated || trunc.Rows != 1 {
		t.Fatalf("truncated: %v %+v", err, trunc)
	}
	// 权限：cs 无 audit.read。
	if _, err := s.ExportAudit(ctxb, Actor{Username: "cs1", Role: RoleCS}, store.Page{}, 10); !errcode.Is(err, errcode.AdminForbidden) {
		t.Fatalf("want forbidden, got %v", err)
	}
}
