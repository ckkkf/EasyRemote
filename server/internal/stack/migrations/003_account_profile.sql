-- ADM-API-03b：账号资料列（昵称、最近登录）。
-- 幂等：ADD COLUMN IF NOT EXISTS，重复启动不会失败。
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS nickname text NOT NULL DEFAULT '';
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS last_login_at timestamptz;
