// Package trace 在 context 中携带请求 trace_id，供领域事件/审计落库。
package trace

import "context"

type key struct{}

// With 返回带 trace_id 的 context。
func With(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, key{}, id)
}

// From 读取 trace_id；缺失返回空串。
func From(ctx context.Context) string {
	v, _ := ctx.Value(key{}).(string)
	return v
}
