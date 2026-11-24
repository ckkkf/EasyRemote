-- ADM-API-02c：会话质量采样（客户端按秒上报，管理端只读）。
CREATE TABLE IF NOT EXISTS session_quality_samples (
    session_id    text NOT NULL,
    ts            timestamptz NOT NULL,
    latency_ms    integer NOT NULL DEFAULT 0,
    loss_permille integer NOT NULL DEFAULT 0,
    fps           integer NOT NULL DEFAULT 0,
    tier          text NOT NULL DEFAULT '',
    relay         boolean NOT NULL DEFAULT false,
    PRIMARY KEY (session_id, ts)
);
CREATE INDEX IF NOT EXISTS session_quality_session_idx ON session_quality_samples(session_id, ts);
