-- ADM-API-04a：设备元信息（客户端上报，管理端展示）。
-- 全部 NOT NULL DEFAULT ''：老行不需要回填，空串表示客户端还没上报。
ALTER TABLE devices ADD COLUMN IF NOT EXISTS device_name text NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN IF NOT EXISTS os_version text NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN IF NOT EXISTS client_version text NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN IF NOT EXISTS nat_type text NOT NULL DEFAULT '';
