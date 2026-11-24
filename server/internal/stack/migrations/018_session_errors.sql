-- ADM-API-03h：会话级错误事件（用于错误码时间线）。
CREATE TABLE IF NOT EXISTS session_errors (
    id         text PRIMARY KEY,
    session_id text NOT NULL,
    code       integer NOT NULL,
    name       text NOT NULL DEFAULT '',
    detail     text NOT NULL DEFAULT '',
    source     text NOT NULL DEFAULT '',
    ts         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS session_errors_session_idx ON session_errors(session_id, ts);
