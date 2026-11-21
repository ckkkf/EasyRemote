-- EasyRemote 首版 schema。
-- 约定：所有时间列用 timestamptz（UTC）；audit_logs 只追加不改。

CREATE TABLE IF NOT EXISTS accounts (
    id          text PRIMARY KEY,
    phone_hash  text NOT NULL UNIQUE,
    phone_enc   text NOT NULL DEFAULT '',
    status      text NOT NULL DEFAULT 'active',
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS devices (
    id           text PRIMARY KEY,
    device_code  text NOT NULL UNIQUE,
    account_id   text NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    platform     text NOT NULL DEFAULT '',
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    online       boolean NOT NULL DEFAULT false,
    token_hash   text NOT NULL DEFAULT '',
    -- 设备长期身份公钥（Ed25519 hex），端到端加密握手验签用
    identity_pubkey text NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS devices_account_idx ON devices(account_id);

CREATE TABLE IF NOT EXISTS verify_codes (
    device_code text PRIMARY KEY,
    code_hash   text NOT NULL,
    expires_at  timestamptz NOT NULL,
    attempts    integer NOT NULL DEFAULT 0,
    locked_till timestamptz
);

CREATE TABLE IF NOT EXISTS trust_edges (
    controller_id text NOT NULL,
    target_id     text NOT NULL,
    expires_at    timestamptz NOT NULL,
    PRIMARY KEY (controller_id, target_id)
);

CREATE TABLE IF NOT EXISTS sessions (
    id             text PRIMARY KEY,
    controller_id  text NOT NULL,
    target_id      text NOT NULL,
    state          text NOT NULL,
    reason         text NOT NULL DEFAULT '',
    started_at     timestamptz NOT NULL DEFAULT now(),
    ended_at       timestamptz,
    auth_expire_at timestamptz
);
CREATE INDEX IF NOT EXISTS sessions_state_idx ON sessions(state);
CREATE INDEX IF NOT EXISTS sessions_pair_idx ON sessions(controller_id, target_id);

CREATE TABLE IF NOT EXISTS admin_users (
    id            text PRIMARY KEY,
    username      text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    totp_secret   text NOT NULL DEFAULT '',
    role          text NOT NULL,
    status        text NOT NULL DEFAULT 'active'
);

-- 审计只允许 INSERT/SELECT：撤销 UPDATE/DELETE 权限由部署脚本负责，
-- 这里用触发器兜底，防止任何路径改写历史。
CREATE TABLE IF NOT EXISTS audit_logs (
    id     text PRIMARY KEY,
    actor  text NOT NULL,
    role   text NOT NULL,
    action text NOT NULL,
    object text NOT NULL,
    before text NOT NULL DEFAULT '',
    after  text NOT NULL DEFAULT '',
    ip     text NOT NULL DEFAULT '',
    result text NOT NULL,
    ts     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS audit_ts_idx ON audit_logs(ts DESC);

CREATE OR REPLACE FUNCTION audit_logs_append_only() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_logs is append-only';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS audit_no_update ON audit_logs;
CREATE TRIGGER audit_no_update BEFORE UPDATE OR DELETE ON audit_logs
    FOR EACH ROW EXECUTE FUNCTION audit_logs_append_only();

-- 文件传输元数据：字节落盘（os.TempDir()/easyremote-files），这里只存索引。
CREATE TABLE IF NOT EXISTS files (
    id           text PRIMARY KEY,
    account_id   text NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    device_id    text NOT NULL DEFAULT '',
    name         text NOT NULL,
    size         bigint NOT NULL,
    sha256       text NOT NULL,
    storage_path text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS files_account_idx ON files(account_id, created_at DESC);

-- 一次性协助码：被控设备生成，主控兑换成 requested 会话。
CREATE TABLE IF NOT EXISTS assist_codes (
    code        text PRIMARY KEY,
    account_id  text NOT NULL,
    target_id   text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    redeemed_at timestamptz
);
CREATE INDEX IF NOT EXISTS assist_expiry_idx ON assist_codes(expires_at);

-- 运行时配置中心：键值对，白名单与取值校验在服务层（internal/appconfig）。
CREATE TABLE IF NOT EXISTS app_config (
    key        text PRIMARY KEY,
    value      text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

