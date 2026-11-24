package adminapi

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/easyremote/server/internal/appconfig"
	"github.com/easyremote/server/internal/contract"
	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/store"
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

// ConfigHistory 返回配置变更历史（config.update / config.rollback 的审计记录，按时间倒序）。
// 审计里存的是「本次改动键的 before/after JSON」，所以历史天然可按 key 还原。
func (s *Service) ConfigHistory(ctx context.Context, a Actor, limit int) (contract.ConfigHistoryResult, error) {
	if !Can(a.Role, "config.read") {
		return contract.ConfigHistoryResult{}, errcode.New(errcode.AdminForbidden, "config.read")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.store.ListAuditPage(ctx, store.Page{Limit: 200})
	if err != nil {
		return contract.ConfigHistoryResult{}, errcode.New(errcode.SrvInternal, err.Error())
	}
	items := make([]contract.ConfigHistoryEntry, 0, limit)
	for _, r := range rows {
		if !strings.HasPrefix(r.Action, "config.update") && !strings.HasPrefix(r.Action, "config.rollback") {
			continue
		}
		items = append(items, contract.ConfigHistoryEntry{
			ID:     r.ID,
			Action: r.Action,
			Actor:  r.Actor,
			Before: r.Before,
			After:  r.After,
			Result: r.Result,
			TS:     r.TS,
		})
		if len(items) >= limit {
			break
		}
	}
	return contract.ConfigHistoryResult{Items: items}, nil
}

// RollbackConfig 把单个配置键回滚到历史值；走同一套校验，审计动作是 config.rollback。
func (s *Service) RollbackConfig(ctx context.Context, a Actor, key, value string) (map[string]string, error) {
	if !Can(a.Role, "config.write") {
		_ = s.audit(ctx, a, "config.rollback", key, "", "", "denied")
		return nil, errcode.New(errcode.AdminForbidden, "config.write")
	}
	if strings.TrimSpace(key) == "" {
		_ = s.audit(ctx, a, "config.rollback", key, "", "", "invalid")
		return nil, errcode.New(errcode.AdminConfigInvalid, "empty key")
	}
	before, err := s.configService().Update(ctx, map[string]string{key: value})
	if err != nil {
		_ = s.audit(ctx, a, "config.rollback", key, "", "", "invalid")
		return nil, err
	}
	after, err := s.configService().All(ctx)
	if err != nil {
		return nil, err
	}
	beforeJSON, _ := json.Marshal(map[string]string{key: before[key]})
	afterJSON, _ := json.Marshal(map[string]string{key: after[key]})
	if err := s.audit(ctx, a, "config.rollback", key, string(beforeJSON), string(afterJSON), "ok"); err != nil {
		return nil, errcode.New(errcode.SrvInternal, "audit write failed")
	}
	return after, nil
}

// configScopeObject 统一审计对象格式：全局 app_config@；平台 app_config@windows；
// 平台+版本 app_config@windows@0.2.13（ADM-API-05c1/05d）。
func configScopeObject(platform, version string) string {
	obj := "app_config@" + platform
	if version != "" {
		obj += "@" + version
	}
	return obj
}

// GetConfigFor 返回指定作用域（"" = 全局）下的生效配置（ADM-API-05c1）。
func (s *Service) GetConfigFor(ctx context.Context, a Actor, platform string) (map[string]string, error) {
	return s.GetConfigScope(ctx, a, platform, "")
}

// GetConfigScope 读取 (platform, version) 作用域（ADM-API-05d）。
func (s *Service) GetConfigScope(ctx context.Context, a Actor, platform, version string) (map[string]string, error) {
	object := configScopeObject(platform, version)
	if !Can(a.Role, "config.read") {
		_ = s.audit(ctx, a, "config.read", object, "", "", "denied")
		return nil, errcode.New(errcode.AdminForbidden, "config.read")
	}
	all, err := s.configService().AllForScope(ctx, platform, version)
	if err != nil {
		_ = s.audit(ctx, a, "config.read", object, "", "", "error")
		return nil, err
	}
	return all, nil
}

// PutConfigFor 在指定作用域写入配置；审计对象带上作用域，避免"改了哪个平台"说不清。
func (s *Service) PutConfigFor(ctx context.Context, a Actor, platform string, values map[string]string) (map[string]string, error) {
	return s.PutConfigScope(ctx, a, platform, "", values)
}

// PutConfigScope 在 (platform, version) 作用域写入并审计（ADM-API-05d）。
func (s *Service) PutConfigScope(ctx context.Context, a Actor, platform, version string, values map[string]string) (map[string]string, error) {
	object := configScopeObject(platform, version)
	if !Can(a.Role, "config.write") {
		_ = s.audit(ctx, a, "config.update", object, "", "", "denied")
		return nil, errcode.New(errcode.AdminForbidden, "config.write")
	}
	if len(values) == 0 {
		_ = s.audit(ctx, a, "config.update", object, "", "", "empty")
		return nil, errcode.New(errcode.AdminConfigInvalid, "empty values")
	}
	before, err := s.configService().AllForScope(ctx, platform, version)
	if err != nil {
		return nil, err
	}
	if _, err := s.configService().UpdateForScope(ctx, platform, version, values); err != nil {
		_ = s.audit(ctx, a, "config.update", object, "", "", "invalid")
		return nil, err
	}
	after, err := s.configService().AllForScope(ctx, platform, version)
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
	if err := s.audit(ctx, a, "config.update", object, string(beforeJSON), string(afterJSON), result); err != nil {
		return nil, errcode.New(errcode.SrvInternal, "audit write failed")
	}
	return after, nil
}
