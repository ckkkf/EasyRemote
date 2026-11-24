-- ADM-API-03c：账号备注（运营备注，写操作进审计）。
CREATE TABLE IF NOT EXISTS account_notes (
    account_id text PRIMARY KEY,
    note       text NOT NULL DEFAULT '',
    updated_by text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now()
);
