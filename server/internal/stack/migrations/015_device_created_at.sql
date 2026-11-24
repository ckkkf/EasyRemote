-- ADM-API-02f6：设备创建时间（A2「24h 新增设备」）。
ALTER TABLE devices ADD COLUMN IF NOT EXISTS created_at timestamptz NOT NULL DEFAULT now();
