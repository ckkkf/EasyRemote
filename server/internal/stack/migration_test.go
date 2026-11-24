package stack

import (
	"strings"
	"testing"
)

// TestLoadMigrationIncludesAllFiles：迁移是拼接执行的，任何一个文件没被 embed 进去，
// 生产库就会缺列/缺表（ADM-API-02c 期间就发生过把迁移放到仓库根目录、实际没生效）。
func TestLoadMigrationIncludesAllFiles(t *testing.T) {
	ddl, err := LoadMigration()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, needle := range []string{
		"CREATE TABLE IF NOT EXISTS accounts",
		"CREATE TABLE IF NOT EXISTS session_quality_samples",
		"ADD COLUMN IF NOT EXISTS nickname",
		"ADD COLUMN IF NOT EXISTS device_name",
	} {
		if !strings.Contains(ddl, needle) {
			t.Fatalf("迁移缺少 %q（检查文件是否放在 server/internal/stack/migrations 并被打包）", needle)
		}
	}
}
