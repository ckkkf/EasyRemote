-- ADM-API-03f2a：会话结束分类落库（便于按"异常结束"筛选）。
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS close_kind text NOT NULL DEFAULT '';
