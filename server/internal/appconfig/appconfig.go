// Package appconfig 是运行时配置中心：键白名单 + 取值校验 + 默认值。
//
// 为什么不直接透传 map[string]string：
//   - 配置会被客户端读取，必须只有白名单键能下发，避免误配的内部开关外泄；
//   - 配置直接决定会话时长与上传上限，取值必须做类型与范围校验，
//     否则一次手滑就能把「单文件 32MiB」改成负数，把上传全打挂。
package appconfig

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/store"
)

// 键常量。新增键必须同时进 Fields 白名单。
const (
	KeySessionMaxMinutes  = "session.max_minutes"
	KeyFileMaxBytes       = "file.max_bytes"
	KeyDiagAutoRefreshSec = "diag.auto_refresh_secs"
	KeyClientForceUpdate  = "client.force_update"
)

// Kind 是取值类型。
type Kind string

const (
	KindInt  Kind = "int"
	KindBool Kind = "bool"
)

// Field 描述一个可配置项。
type Field struct {
	Key     string `json:"key"`
	Kind    Kind   `json:"kind"`
	Default string `json:"default"`
	Min     int64  `json:"min,omitempty"`
	Max     int64  `json:"max,omitempty"`
	Help    string `json:"help"`
}

// Fields 是白名单，顺序即管理端表单展示顺序。
var Fields = []Field{
	{Key: KeySessionMaxMinutes, Kind: KindInt, Default: "720", Min: 1, Max: 1440, Help: "单次会话最长时长（分钟）"},
	{Key: KeyFileMaxBytes, Kind: KindInt, Default: "33554432", Min: 1024, Max: 33554432, Help: "单文件上传上限（字节，上限 32 MiB）"},
	{Key: KeyDiagAutoRefreshSec, Kind: KindInt, Default: "30", Min: 5, Max: 300, Help: "诊断页自动刷新间隔（秒）"},
	{Key: KeyClientForceUpdate, Kind: KindBool, Default: "false", Help: "true 时客户端提示必须升级"},
}

// fieldByKey 索引。
var fieldByKey = func() map[string]Field {
	m := make(map[string]Field, len(Fields))
	for _, f := range Fields {
		m[f.Key] = f
	}
	return m
}()

// IsKnown 判断键是否在白名单内。
func IsKnown(key string) bool {
	_, ok := fieldByKey[key]
	return ok
}

// Validate 校验键与值。未知键、类型不符、越界都返回 ADMIN_CONFIG_INVALID。
func Validate(key, value string) error {
	f, ok := fieldByKey[key]
	if !ok {
		return errcode.New(errcode.AdminConfigInvalid, "unknown key: "+key)
	}
	switch f.Kind {
	case KindInt:
		n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return errcode.New(errcode.AdminConfigInvalid, key+" must be an integer")
		}
		if n < f.Min || n > f.Max {
			return errcode.New(errcode.AdminConfigInvalid,
				fmt.Sprintf("%s out of range [%d,%d]", key, f.Min, f.Max))
		}
	case KindBool:
		if _, err := strconv.ParseBool(strings.TrimSpace(value)); err != nil {
			return errcode.New(errcode.AdminConfigInvalid, key+" must be true or false")
		}
	}
	return nil
}

// Service 读写配置，未设置的键回落到默认值。
type Service struct {
	store store.Store
	now   func() time.Time
}

// New 创建配置服务。
func New(s store.Store) *Service {
	return &Service{store: s, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock 仅测试使用。
func (s *Service) SetClock(f func() time.Time) { s.now = f }

// All 返回「默认值 + 已覆盖值」的扁平映射。
func (s *Service) All(ctx context.Context) (map[string]string, error) {
	stored, err := s.store.ListConfig(ctx)
	if err != nil {
		return nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	out := make(map[string]string, len(Fields))
	for _, f := range Fields {
		if v, ok := stored[f.Key]; ok {
			out[f.Key] = v
		} else {
			out[f.Key] = f.Default
		}
	}
	return out, nil
}

// Get 读单个键（带默认值）。
func (s *Service) Get(ctx context.Context, key string) (string, error) {
	if !IsKnown(key) {
		return "", errcode.New(errcode.AdminConfigInvalid, "unknown key: "+key)
	}
	all, err := s.All(ctx)
	if err != nil {
		return "", err
	}
	return all[key], nil
}

// GetInt 读整数键。
func (s *Service) GetInt(ctx context.Context, key string) (int64, error) {
	v, err := s.Get(ctx, key)
	if err != nil {
		return 0, err
	}
	n, convErr := strconv.ParseInt(v, 10, 64)
	if convErr != nil {
		f := fieldByKey[key]
		n, _ = strconv.ParseInt(f.Default, 10, 64)
	}
	return n, nil
}

// GetBool 读布尔键。
func (s *Service) GetBool(ctx context.Context, key string) (bool, error) {
	v, err := s.Get(ctx, key)
	if err != nil {
		return false, err
	}
	b, convErr := strconv.ParseBool(v)
	if convErr != nil {
		return false, nil
	}
	return b, nil
}

// Update 批量写。任一键非法则整体不写，避免「改了一半」的中间态。
//
// 返回值是写前快照，供调用方落审计（before/after）。
func (s *Service) Update(ctx context.Context, values map[string]string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, errcode.New(errcode.AdminConfigInvalid, "empty payload")
	}
	keys := make([]string, 0, len(values))
	for k, v := range values {
		if err := Validate(k, v); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	before, err := s.All(ctx)
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		if err := s.store.PutConfig(ctx, k, strings.TrimSpace(values[k])); err != nil {
			return nil, errcode.New(errcode.SrvInternal, err.Error())
		}
	}
	return before, nil
}

// Public 返回给客户端读取的嵌套结构（白名单键，全部就绪）。
//
// 客户端拿到的永远是「可用值」而不是「配置项」：缺省键已经填充默认值，
// 客户端不需要再实现一遍默认逻辑。
func (s *Service) Public(ctx context.Context) (map[string]any, error) {
	all, err := s.All(ctx)
	if err != nil {
		return nil, err
	}
	sessionMax, _ := strconv.ParseInt(all[KeySessionMaxMinutes], 10, 64)
	fileMax, _ := strconv.ParseInt(all[KeyFileMaxBytes], 10, 64)
	autoRefresh, _ := strconv.ParseInt(all[KeyDiagAutoRefreshSec], 10, 64)
	forceUpdate, _ := strconv.ParseBool(all[KeyClientForceUpdate])
	return map[string]any{
		"session": map[string]any{"max_minutes": sessionMax},
		"file":    map[string]any{"max_bytes": fileMax},
		"diag":    map[string]any{"auto_refresh_secs": autoRefresh},
		"client":  map[string]any{"force_update": forceUpdate},
	}, nil
}

// Flatten 把嵌套结构摊平（管理端表单与测试用）。
func Flatten(nested map[string]any) map[string]string {
	out := map[string]string{}
	for section, raw := range nested {
		inner, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		for name, v := range inner {
			switch t := v.(type) {
			case int64:
				out[section+"."+name] = strconv.FormatInt(t, 10)
			case bool:
				out[section+"."+name] = strconv.FormatBool(t)
			case string:
				out[section+"."+name] = t
			}
		}
	}
	return out
}
