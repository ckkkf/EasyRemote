-- ADM-API-05d：配置按版本作用域（platform + version；version 为空表示"该平台任意版本"）。
ALTER TABLE config_overrides ADD COLUMN IF NOT EXISTS version text NOT NULL DEFAULT '';
ALTER TABLE config_overrides DROP CONSTRAINT IF EXISTS config_overrides_pkey;
ALTER TABLE config_overrides ADD PRIMARY KEY (key, platform, version);
