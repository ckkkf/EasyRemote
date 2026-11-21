-- 设备「临时密码」：长期有效、校验不消费，被控端本地保存明文并可复制/查看。
-- 与一次性验证码（verify_codes 表）并存：老流程不动，新流程走这一列。
ALTER TABLE devices ADD COLUMN IF NOT EXISTS password_hash text NOT NULL DEFAULT '';
