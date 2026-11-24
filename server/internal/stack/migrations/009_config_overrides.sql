-- ADM-API-05c1：配置项平台作用域覆盖（'' = 全局，其余为平台名）。
CREATE TABLE IF NOT EXISTS config_overrides (
    key      text NOT NULL,
    platform text NOT NULL DEFAULT '',
    value    text NOT NULL,
    PRIMARY KEY (key, platform)
);
