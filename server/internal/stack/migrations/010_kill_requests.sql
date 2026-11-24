-- ADM-API-07b1：强制中断双人确认请求（高危操作需要第二人批准）。
CREATE TABLE IF NOT EXISTS kill_requests (
    id           text PRIMARY KEY,
    session_id   text NOT NULL,
    reason       text NOT NULL,
    requested_by text NOT NULL,
    requested_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    status       text NOT NULL DEFAULT 'pending',  -- pending / confirmed / expired
    confirmed_by text NOT NULL DEFAULT '',
    confirmed_at timestamptz
);
CREATE INDEX IF NOT EXISTS kill_requests_status_idx ON kill_requests(status, requested_at DESC);
