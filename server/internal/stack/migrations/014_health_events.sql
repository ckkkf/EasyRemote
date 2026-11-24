-- ADM-API-02f5：客户端崩溃与重连事件上报（稳定性组最后两项）。
CREATE TABLE IF NOT EXISTS client_health_events (
    id         text PRIMARY KEY,
    session_id text NOT NULL,
    device_id  text NOT NULL DEFAULT '',
    kind       text NOT NULL,  -- crash / reconnect_attempt / reconnect_success
    ts         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS client_health_ts_idx ON client_health_events(ts DESC);
