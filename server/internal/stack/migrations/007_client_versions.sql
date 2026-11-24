-- ADM-API-06b：客户端版本与灰度发布（A11 后端）。
CREATE TABLE IF NOT EXISTS client_versions (
    id              text PRIMARY KEY,
    platform        text NOT NULL,
    version         text NOT NULL,
    status          text NOT NULL DEFAULT 'draft',
    rollout_percent integer NOT NULL DEFAULT 0,
    force_update    boolean NOT NULL DEFAULT false,
    notes           text NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (platform, version)
);
CREATE INDEX IF NOT EXISTS client_versions_platform_idx ON client_versions(platform, created_at DESC);
