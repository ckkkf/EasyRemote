-- ADM-API-02f2：建连尝试埋点（成功率口径：active / attempts）。
CREATE TABLE IF NOT EXISTS connect_attempts (
    session_id    text PRIMARY KEY,
    account_id    text NOT NULL,
    controller_id text NOT NULL,
    target_id     text NOT NULL,
    outcome       text NOT NULL DEFAULT 'requested',  -- requested / active
    link          text NOT NULL DEFAULT 'unknown',    -- unknown / relay（p2p 需客户端上报）
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS connect_attempts_created_idx ON connect_attempts(created_at DESC);
