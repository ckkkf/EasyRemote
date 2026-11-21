package adminapi

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/easyremote/server/internal/appconfig"
	"github.com/easyremote/server/internal/errcode"
)

// ConfigFields 返回配置中心的可编辑项（管理端表单 schema）。
func (s *Service) ConfigFields(ctx context.Context, a Actor) ([]appconfig.Field, error) {
	if !Can(a.Role, "config.read") {
		return nil, errcode.New(errcode.AdminForbidden, "config.read")
	}
	return appconfig.Fields, nil
}

// GetConfig 返回「默认值 + 已覆盖值」的扁平映射。
func (s *Service) GetConfig(ctx context.Context, a Actor) (map[string]string, error) {
	if !Can(a.Role, "config.read") {
		_ = s.audit(ctx, a, "config.read", "app_config", "", "", "denied")
		return nil, errcode.New(errcode.AdminForbidden, "config.read")
	}
	all, err := s.configService().All(ctx)
	if err != nil {
		_ = s.audit(ctx, a, "config.read", "app_config", "", "", "error")
		return nil, err
	}
	return all, nil
}

// PutConfig 批量更新配置，并把「每个被改动的键」的 before/after 写进审计。
//
// 审计粒度按 key 记录：只写整张表的前后快照会让审计日志不可读，
// 事后也没法回答「谁把上传上限从 32MiB 改成 1KiB」。
func (s *Service) PutConfig(ctx context.Context, a Actor, values map[string]string) (map[string]string, error) {
	if !Can(a.Role, "config.write") {
		_ = s.audit(ctx, a, "config.update", "app_config", "", "", "denied")
		return nil, errcode.New(errcode.AdminForbidden, "config.write")
	}
	if len(values) == 0 {
		_ = s.audit(ctx, a, "config.update", "app_config", "", "", "empty")
		return nil, errcode.New(errcode.AdminConfigInvalid, "empty values")
	}
	before, err := s.configService().Update(ctx, values)
	if err != nil {
		_ = s.audit(ctx, a, "config.update", "app_config", "", "", "invalid")
		return nil, err
	}
	after, err := s.configService().All(ctx)
	if err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	beforeSnap := map[string]string{}
	afterSnap := map[string]string{}
	changed := false
	for _, k := range keys {
		b, n := before[k], after[k]
		beforeSnap[k], afterSnap[k] = b, n
		if b != n {
			changed = true
		}
	}
	beforeJSON, _ := json.Marshal(beforeSnap)
	afterJSON, _ := json.Marshal(afterSnap)
	result := "ok"
	if !changed {
		result = "noop"
	}
	if err := s.audit(ctx, a, "config.update", "app_config", string(beforeJSON), string(afterJSON), result); err != nil {
		return nil, errcode.New(errcode.SrvInternal, "audit write failed")
	}
	return after, nil
}
