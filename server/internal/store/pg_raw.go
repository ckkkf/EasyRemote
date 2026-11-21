package store

import "context"

// ExecRaw 直接执行 DDL/维护语句（迁移、清表），仅供测试与初始化使用。
func (p *PG) ExecRaw(ctx context.Context, sql string) (int64, error) {
	tag, err := p.pool.Exec(ctx, sql)
	if err != nil {
		return 0, mapErr(err)
	}
	return tag.RowsAffected(), nil
}
