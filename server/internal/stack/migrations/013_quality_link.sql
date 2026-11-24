-- ADM-API-02f4：客户端上报链路类型与目标帧率，用于 P2P 命中率/帧率达成率。
ALTER TABLE session_quality_samples ADD COLUMN IF NOT EXISTS link text NOT NULL DEFAULT '';
ALTER TABLE session_quality_samples ADD COLUMN IF NOT EXISTS target_fps integer NOT NULL DEFAULT 0;
