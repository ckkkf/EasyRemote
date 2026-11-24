-- ADM-API-06c：风控黑名单与异常连接告警（A10）。
CREATE TABLE IF NOT EXISTS blacklist_entries (
    id         text PRIMARY KEY,
    kind       text NOT NULL,           -- ip / device / account
    value      text NOT NULL,
    note       text NOT NULL DEFAULT '',
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    created_by text NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS blacklist_kind_value_idx ON blacklist_entries(kind, value);

CREATE TABLE IF NOT EXISTS risk_alerts (
    id      text PRIMARY KEY,
    kind    text NOT NULL,              -- blacklist_block
    subject text NOT NULL,
    detail  text NOT NULL DEFAULT '',
    ts      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS risk_alerts_ts_idx ON risk_alerts(ts DESC);
