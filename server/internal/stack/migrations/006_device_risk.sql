-- ADM-API-04c：设备风险标记（运营/风控人工标记，列表可见，操作进审计）。
ALTER TABLE devices ADD COLUMN IF NOT EXISTS risk_flag boolean NOT NULL DEFAULT false;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS risk_reason text NOT NULL DEFAULT '';
